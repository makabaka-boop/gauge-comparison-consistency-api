// Command api 启动计量校准网约束检查的纯后端 JSON 服务。
package main

import (
	"log"
	"net/http"
	"os"

	"gaugeblock/internal/api"
)

func main() {
	addr := ":" + port()
	log.Printf("gaugeblock api listening on %s", addr)
	if err := http.ListenAndServe(addr, api.NewHandler()); err != nil {
		log.Fatal(err)
	}
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
