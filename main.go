package main

import (
	"github.com/gin-gonic/gin"
	"prometheus/prometheus"
)

func main() {
	r := gin.New()
	p := prometheus.NewPrometheus("gin")
	//p.SetListenAddress(":3333")

	p.Use(r)
	r.GET("/", func(c *gin.Context) {
		c.JSON(200, "Hello world!")
	})

	r.Run(":29090")
}
