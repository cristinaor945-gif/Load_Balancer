package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
)

func main() {
  router := gin.Default()
  router.GET("/ping", func(c *gin.Context) {
    c.JSON(200, gin.H{
      "message": "pong",
    })
  });
  router.POST("/post", func(c *gin.Context) {
    var body struct{
      Key string;
      Value string;
    };
    if err := c.BindJSON(&body); err != nil{
      c.JSON(400, gin.H{
        "error": err.Error(),
      });
      return;
    };
    fmt.Print(body.Key,body.Value);

    c.JSON(200, gin.H{
      "message": "success",
      "key": body.Key,
      "value": body.Value,
    });
  });
  router.Run()
};
