package main

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Pair struct {
	BaseUrl  string
	Priority string
	count    int
	index    int
}
type Minheap []*Pair

type Config struct {
	Port                  int             `json:"port"`
	HealthCheckIntervalMs int             `json:"health_check_interval_ms"`
	HealthCheckPath       string          `json:"health_check_path"`
	Backends              []BackendConfig `json:"backends"`
}

type BackendConfig struct {
	URL      string `json:"url"`
	Priority string `json:"priority"`
}

var config *Config

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var mu sync.Mutex
var deadMu sync.Mutex

func (h Minheap) Len() int { return len(h) }

func (h Minheap) Less(i, j int) bool {
	return h[i].count < h[j].count
}
func (h Minheap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *Minheap) Push(x interface{}) {
	item := x.(*Pair)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *Minheap) Pop() interface{} {
	old := *h
	n := len(old)

	item := old[n-1]
	item.index = -1
	*h = old[:n-1]

	return item
}

var h *Minheap
var list [](*Pair)
var baselist [](*Pair)
var Dead map[*Pair]bool

func healthCheck() {
	client := http.Client{Timeout: time.Second}

	for _, v := range baselist {
		req, err := http.NewRequest("GET", v.BaseUrl+config.HealthCheckPath, nil)
		if err != nil {
			panic(err)
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		if err != nil {
			if err == io.EOF {
				continue
			}
			deadMu.Lock()
			Dead[v] = true
			deadMu.Unlock()
			mu.Lock()
			if v.index != -1 {
				heap.Remove(h, v.index)
			}

			mu.Unlock()
			continue
		}
		deadMu.Lock()
		wasDead := Dead[v]
		Dead[v] = false
		deadMu.Unlock()
		if wasDead {
			mu.Lock()
			heap.Push(h, v)
			mu.Unlock()
		}
	}
}
func getBaseurlLeastConn() (*Pair, error) {
	mu.Lock()
	defer mu.Unlock()
	var server *Pair
	if h.Len() == 0 {
		return nil, errors.New("No server Available")
	}
	server = heap.Pop(h).(*Pair)
	server.count++
	heap.Push(h, server)
	return server, nil
}

var idx int

func getBaseurlWRR() string {
	var baseurl string
	baseurl = list[(idx+1)%len(list)].BaseUrl
	idx++
	return baseurl
}
func releaseConn(server *Pair) {
	mu.Lock()
	defer mu.Unlock()
	server.count--
	if server.index != -1 {
		heap.Fix(h, server.index)
	}

}
func handleConn(conn net.Conn) {

	clientIp := conn.RemoteAddr().String()
	reader := bufio.NewReader(conn)

	defer conn.Close()
	client := &http.Client{}

	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			panic(err)
		}
		for {
			server, err := getBaseurlLeastConn()
			//fmt.Println(Dead[server]);
			if err != nil {
				if err == io.EOF {
					return
				}
				panic(err)
			}

			fmt.Println("Method:", req.Method)
			fmt.Println("Path:", req.URL.Path)
			fmt.Println("Host:", req.Host)
			fmt.Println("URL:", req.URL)
			fmt.Println(string(string(req.RemoteAddr)))
			fmt.Println(clientIp)
			fmt.Println(req.Proto)
			requestID := uuid.New().String()
			req.Header.Add("X-Forwaded-For", clientIp)
			req.Header.Add("X-Forwarded-Proto", req.Proto)
			req.Header.Add("X-Request-ID", requestID)

			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)

			sentreq, err := http.NewRequestWithContext(ctx, req.Method, server.BaseUrl+req.URL.Path, req.Body)
			sentreq.Header = req.Header.Clone()
			if err != nil {
				if err == io.EOF {
					return
				}
				panic(err)
			}
			resp, err := client.Do(sentreq)
			cancel()
			if err != nil {
				deadMu.Lock()
				Dead[server] = true
				deadMu.Unlock()
				mu.Lock()
				if server.index != -1 {
					heap.Remove(h, server.index)
				}
				mu.Unlock()
				releaseConn(server)
				continue
			}

			err = resp.Write(conn)
			if err != nil {
				panic(err)
			}
			resp.Body.Close()
			releaseConn(server)
			break
		}
	}

}

func main() {
	var err error
	config, err = loadConfig("config.json")
	if err != nil {
		fmt.Println("No config.json found or failed to load. Please configure via the admin dashboard.")
	} else {
		err = startLoadBalancer()
		if err != nil {
			fmt.Printf("Failed to auto-start load balancer: %s\n", err)
		}
	}

	startAdminServer()
}
