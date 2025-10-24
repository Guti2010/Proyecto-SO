package main

import (
	"log"
	"os"
	"os/signal" 
	"strconv"
	"syscall"   
	"so-http10-demo/internal/router"
	"so-http10-demo/internal/server"
)

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}


func main() {
	router.InitPools(map[string]int{
	// básicos
	"workers.sleep": getenvInt("WORKERS_SLEEP", 2),
	"queue.sleep":   getenvInt("QUEUE_SLEEP", 8),
	"workers.spin":  getenvInt("WORKERS_SPIN", 2),
	"queue.spin":    getenvInt("QUEUE_SPIN", 8),

	// CPU
	"workers.isprime":    getenvInt("WORKERS_ISPRIME", 2),
	"queue.isprime":      getenvInt("QUEUE_ISPRIME", 64),
	"workers.factor":     getenvInt("WORKERS_FACTOR", 2),
	"queue.factor":       getenvInt("QUEUE_FACTOR", 64),
	"workers.pi":         getenvInt("WORKERS_PI", 1),
	"queue.pi":           getenvInt("QUEUE_PI", 8),
	"workers.mandelbrot": getenvInt("WORKERS_MANDELBROT", 1),
	"queue.mandelbrot":   getenvInt("QUEUE_MANDELBROT", 4),
	"workers.matrixmul":  getenvInt("WORKERS_MATRIXMUL", 1),
	"queue.matrixmul":    getenvInt("QUEUE_MATRIXMUL", 8),

	// IO
	"workers.wordcount":  getenvInt("WORKERS_WORDCOUNT", 2),
	"queue.wordcount":    getenvInt("QUEUE_WORDCOUNT", 64),
	"workers.grep":       getenvInt("WORKERS_GREP", 2),
	"queue.grep":         getenvInt("QUEUE_GREP", 64),
	"workers.hashfile":   getenvInt("WORKERS_HASHFILE", 2),
	"queue.hashfile":     getenvInt("QUEUE_HASHFILE", 64),
	"workers.sortfile":  getenvInt("WORKERS_SORTFILE", 1),
	"queue.sortfile":    getenvInt("QUEUE_SORTFILE", 4),
	"workers.compress":  getenvInt("WORKERS_COMPRESS", 1),
	"queue.compress":    getenvInt("QUEUE_COMPRESS", 4),
	})

	// cierre ordenado opcional
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-quit
        router.Close()
        os.Exit(0)
    }()

	log.Println("HTTP/1.0 server starting on :8080")
	if err := server.ListenAndServe(":8080"); err != nil {
		log.Fatalf("listen failed: %v", err)
	}
}
