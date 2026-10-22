package main

import (
	"fmt"
	"os"
	"starlight/backend/internal/accounts"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: lhc-migrate accounts.db")
	}
	s, e := accounts.Open(os.Args[1])
	if e != nil {
		panic(e)
	}
	if e = s.Close(); e != nil {
		panic(e)
	}
	fmt.Println("account catalog migration complete")
}
