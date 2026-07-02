package main

import (
	"container/heap"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
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
			continue
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
	if h.Len() == 0 {
		return nil, errors.New("no server available")
	}
	server := heap.Pop(h).(*Pair)
	server.count++
	heap.Push(h, server)
	return server, nil
}

var idx int

func releaseConn(server *Pair) {
	mu.Lock()
	defer mu.Unlock()
	server.count--
	if server.index != -1 {
		heap.Fix(h, server.index)
	}
}

func main() {
	var err error
	config, err = loadConfig("config.json")
	if err != nil {
		fmt.Println("No config.json found. Please configure via the admin dashboard.")
	} else {
		if err := initBalancer(); err != nil {
			fmt.Printf("Failed to init balancer: %s\n", err)
		} else {
			startHealthChecker()
			isRunning = true
			fmt.Printf("Load Balancer ready with %d backends\n", len(config.Backends))
		}
	}

	startAdminServer()
}
