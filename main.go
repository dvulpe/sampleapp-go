package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

var (
	serverPort    int
	metricsPort   int
	stopTimeout   time.Duration
	totalRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total http requests",
		},
		[]string{"code"},
	)
	durations = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_requests_duration",
			Help:    "Duration of http requests",
			Buckets: prometheus.ExponentialBuckets(0.001, 10, 5),
		},
		[]string{"code"},
	)
)

func init() {
	prometheus.MustRegister(totalRequests)
	prometheus.MustRegister(durations)
	flag.IntVar(&metricsPort, "metrics-port", 8000, "Port to listen to for metrics")
	flag.IntVar(&serverPort, "server-port", 8080, "Port to listen for http requests")
	flag.DurationVar(&stopTimeout, "stop-timeout", 10*time.Second, "Server stop timeout")
	rand.Seed(time.Now().UnixNano())
}

func main() {
	flag.Parse()
	successRate, err := strconv.Atoi(os.Getenv("SUCCESS_RATE"))
	if err != nil {
		log.Fatalf("could not parse succes rate: %v", err)
	}

	var stopCh = make(chan int)
	var wg = new(sync.WaitGroup)
	wg.Add(2)
	go startServer(createHttpServer(successRate), stopCh, wg)
	go startServer(createMetricsServer(), stopCh, wg)

	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)

	go func() {
		<-c
		log.Println("About to stop server")
		close(stopCh)
	}()
	wg.Wait()
	log.Printf("All stopped.")
}

func startServer(srv *http.Server, stopCh chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	go func() {
		log.Printf("Starting server on %v", srv.Addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("boom: %v", err)
		}
	}()
	<-stopCh
	srv.SetKeepAlivesEnabled(false)
	time.Sleep(10 * time.Second) // give k8s some time to sync services
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()

	log.Printf("Shutting down server on %v in %v\n", srv.Addr, stopTimeout)
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Failed shutdown: %v", err)
	} else {
		log.Printf("Server %v stopped", srv.Addr)
	}
}

func createHttpServer(successRate int) *http.Server {
	mux := http.NewServeMux()
	handler := promhttp.InstrumentHandlerDuration(
		durations,
		promhttp.InstrumentHandlerCounter(
			totalRequests,
			Handler(successRate),
		),
	)
	mux.HandleFunc("/", handler)
	srv := &http.Server{
		Handler:      mux,
		Addr:         fmt.Sprintf(":%d", serverPort),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	return srv
}

func Handler(successRate int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rand.Intn(100) > successRate {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Fail")
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Hello World!\n")
		}
	}
}

func createMetricsServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		promhttp.Handler().ServeHTTP(w, r)
	})
	mux.HandleFunc("/liveness", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})
	mux.HandleFunc("/readiness", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})
	return &http.Server{
		Handler:      mux,
		Addr:         fmt.Sprintf(":%d", metricsPort),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
}
