package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	lbCancel  context.CancelFunc
	isRunning bool
	runMu     sync.Mutex
)

// Server status response structure
type BackendStatusResponse struct {
	URL         string `json:"url"`
	Priority    string `json:"priority"`
	Connections int    `json:"connections"`
	IsDead      bool   `json:"isDead"`
}

type StatusResponse struct {
	Running               bool                    `json:"running"`
	Port                  int                     `json:"port"`
	HealthCheckPath       string                  `json:"health_check_path"`
	HealthCheckIntervalMs int                     `json:"health_check_interval_ms"`
	Backends              []BackendStatusResponse `json:"backends"`
}

// initBalancer sets up the in-memory backend state from config (no TCP listener needed)
func initBalancer() error {
	if config == nil {
		return errors.New("no configuration loaded")
	}

	idx = 0
	Dead = make(map[*Pair]bool)

	baselist = make([]*Pair, len(config.Backends))
	for i, bk := range config.Backends {
		baselist[i] = &Pair{BaseUrl: bk.URL, Priority: bk.Priority, count: 0}
	}

	list = nil
	for _, v := range baselist {
		count, _ := strconv.Atoi(v.Priority)
		for count > 0 {
			list = append(list, v)
			count -= 1
		}
	}

	h = &Minheap{}
	heap.Init(h)
	for _, server := range baselist {
		heap.Push(h, server)
	}

	return nil
}

// startHealthChecker launches the background health check polling goroutine
func startHealthChecker() {
	var ctx context.Context
	ctx, lbCancel = context.WithCancel(context.Background())
	ticker := time.NewTicker(time.Duration(config.HealthCheckIntervalMs) * time.Millisecond)

	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				healthCheck()
			}
		}
	}()
}

// stopBalancer cancels the health checker goroutine
func stopBalancer() {
	if lbCancel != nil {
		lbCancel()
	}
	isRunning = false
	fmt.Println("Load Balancer stopped")
}

// proxyHandler picks the least-connected backend and reverse-proxies the request
func proxyHandler(c *gin.Context) {
	server, err := getBaseurlLeastConn()
	if err != nil {
		c.String(http.StatusServiceUnavailable, "No healthy backend available: %s", err.Error())
		return
	}

	target, err := url.Parse(server.BaseUrl)
	if err != nil {
		releaseConn(server)
		c.String(http.StatusInternalServerError, "Invalid backend URL")
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		deadMu.Lock()
		Dead[server] = true
		deadMu.Unlock()
		mu.Lock()
		if server.index != -1 {
			heap.Remove(h, server.index)
		}
		mu.Unlock()
		releaseConn(server)
		http.Error(w, "Backend unavailable", http.StatusBadGateway)
	}

	// Inject forwarding headers
	c.Request.Header.Set("X-Forwarded-For", c.ClientIP())
	c.Request.Header.Set("X-Forwarded-Proto", c.Request.Proto)

	proxy.ServeHTTP(c.Writer, c.Request)
	releaseConn(server)
}

func startAdminServer() {
	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	})

	// Admin API routes
	r.GET("/api/status", func(c *gin.Context) {
		runMu.Lock()
		res := StatusResponse{Running: isRunning}
		if config != nil {
			res.Port = config.Port
			res.HealthCheckPath = config.HealthCheckPath
			res.HealthCheckIntervalMs = config.HealthCheckIntervalMs
		}

		deadMu.Lock()
		res.Backends = make([]BackendStatusResponse, len(baselist))
		for i, v := range baselist {
			res.Backends[i] = BackendStatusResponse{
				URL:         v.BaseUrl,
				Priority:    v.Priority,
				Connections: v.count,
				IsDead:      Dead[v],
			}
		}
		deadMu.Unlock()
		runMu.Unlock()

		c.JSON(200, res)
	})

	r.POST("/api/config", func(c *gin.Context) {
		var newCfg Config
		if err := c.ShouldBindJSON(&newCfg); err != nil {
			c.String(400, "Invalid configuration: %s", err.Error())
			return
		}

		file, err := os.Create("config.json")
		if err != nil {
			c.String(500, "Failed to write config.json: %s", err.Error())
			return
		}
		defer file.Close()

		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(newCfg); err != nil {
			c.String(500, "Failed to encode config: %s", err.Error())
			return
		}

		runMu.Lock()
		config = &newCfg
		runMu.Unlock()

		// Stop old health checker and reinitialize
		stopBalancer()
		if err := initBalancer(); err != nil {
			c.String(500, "Failed to init balancer: %s", err.Error())
			return
		}
		startHealthChecker()
		isRunning = true

		c.String(200, "Config updated and Load Balancer restarted")
	})

	r.POST("/api/stop", func(c *gin.Context) {
		stopBalancer()
		c.String(200, "Load Balancer stopped")
	})

	// Static frontend assets
	r.Static("/assets", "./frontend/dist/assets")
	r.StaticFile("/favicon.ico", "./frontend/dist/favicon.ico")
	r.StaticFile("/vite.svg", "./frontend/dist/vite.svg")

	// React SPA root
	r.GET("/", func(c *gin.Context) {
		if _, err := os.Stat("./frontend/dist/index.html"); err == nil {
			c.File("./frontend/dist/index.html")
		} else {
			c.String(200, "<h3>Frontend not built. Run 'npm run build' in the frontend/ directory.</h3>")
		}
	})

	// All other routes → reverse proxy to backends
	r.NoRoute(func(c *gin.Context) {
		if isRunning {
			proxyHandler(c)
		} else {
			c.String(http.StatusServiceUnavailable, "Load balancer is not running. Configure and start it via the admin panel.")
		}
	})

	// Determine port from environment variable (Render injects $PORT)
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}

	fmt.Printf("Server listening on port %s\n", port)
	fmt.Printf("Admin dashboard: http://localhost:%s\n", port)
	if err := r.Run(":" + port); err != nil {
		fmt.Printf("Server error: %s\n", err)
	}
}
