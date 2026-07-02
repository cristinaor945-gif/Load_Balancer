package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	lbListener  net.Listener
	lbCancel    context.CancelFunc
	lbWaitGroup sync.WaitGroup
	isRunning   bool
	runMu       sync.Mutex
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


func startLoadBalancer() error {
	runMu.Lock()
	defer runMu.Unlock()
	if isRunning {
		return errors.New("load balancer is already running")
	}

	if config == nil {
		return errors.New("no configuration loaded")
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		return err
	}
	lbListener = ln

	// Reinitialize load balancer state variables
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

	// Create cancellable context for healthcheck
	var ctx context.Context
	ctx, lbCancel = context.WithCancel(context.Background())

	ticker := time.NewTicker(time.Duration(config.HealthCheckIntervalMs) * time.Millisecond)

	// Spin health checking routine
	lbWaitGroup.Add(1)
	go func() {
		defer lbWaitGroup.Done()
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

	lbWaitGroup.Add(1)
	go func() {
		defer lbWaitGroup.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConn(conn)
		}
	}()

	isRunning = true
	fmt.Printf("Started Load Balancer on port %d\n", config.Port)
	return nil
}

// stopLoadBalancer terminates the connection listener and healthcheck system
func stopLoadBalancer() {
	runMu.Lock()
	defer runMu.Unlock()
	if !isRunning {
		return
	}

	if lbListener != nil {
		lbListener.Close()
	}

	if lbCancel != nil {
		lbCancel()
	}

	lbWaitGroup.Wait()
	isRunning = false
	fmt.Println("[Balancify] Stopped Load Balancer")
}


func startAdminServer() {
	// Set Gin to ReleaseMode to avoid debug prints showing up in stdout
	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()

	// CORS middleware configuration
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

	r.GET("/api/status", func(c *gin.Context) {
		runMu.Lock()
		res := StatusResponse{
			Running: isRunning,
		}
		if config != nil {
			res.Port = config.Port
			res.HealthCheckPath = config.HealthCheckPath
			res.HealthCheckIntervalMs = config.HealthCheckIntervalMs
		}

		// Fill backend status list
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
			c.String(500, "Failed to encode configuration: %s", err.Error())
			return
		}

		runMu.Lock()
		config = &newCfg
		runMu.Unlock()

		// Reload/Restart balancer
		stopLoadBalancer()
		err = startLoadBalancer()
		if err != nil {
			c.String(500, "Failed to start Load Balancer: %s", err.Error())
			return
		}

		c.String(200, "Config updated and Load Balancer restarted")
	})

	// Stop API
	r.POST("/api/stop", func(c *gin.Context) {
		stopLoadBalancer()
		c.String(200, "Load Balancer stopped")
	})

	// Serve Frontend React Static Files Fallback
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		fullPath := "./frontend/dist" + path
		if _, err := os.Stat(fullPath); err == nil {
			c.File(fullPath)
		} else {
			// Single page app fallback: serve index.html
			if _, err := os.Stat("./frontend/dist/index.html"); err == nil {
				c.File("./frontend/dist/index.html")
			} else {
				c.Header("Content-Type", "text/html")
				c.String(200, "<h3>Control Panel frontend not built yet. Run 'npm run build' inside 'frontend/' to compile it.</h3>")
			}
		}
	})

	fmt.Println("[Balancify] Control Panel Server listening on http://localhost:9000")
	if err := r.Run(":9000"); err != nil {
		fmt.Printf("[Balancify] Admin server error: %s\n", err)
	}
}
