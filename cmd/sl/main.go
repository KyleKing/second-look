package main

import (
	"fmt"
	"time"

	"github.com/kyleking/aragonite/cache"
)

func main() {
	c := cache.NewTTLCache[string](time.Minute)
	c.Set("kyleking/second-look#1", cache.NoStamp, "wired")
	got, ok := c.Get("kyleking/second-look#1", cache.NoStamp)
	fmt.Println(got, ok)
}
