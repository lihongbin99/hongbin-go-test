package main

import (
	"fmt"
	"time"
)

func main() {
	ticker := time.NewTicker(3 * time.Second)
	for true {
		<-ticker.C
		fmt.Println("a")
	}
}
