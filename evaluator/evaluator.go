package main

import (
	"fmt"
	"os"
	"strconv"
)

type Flag struct {
	State string `json:"state"`
}

type Store struct {
	Flags map[string]Flag `json:"flags"`
}

var counter = 0

func main() {
	state := "ENABLED"
	if len(os.Args) > 1 {
		state = os.Args[1]
	}

	store := Store{Flags: make(map[string]Flag)}

	store.Flags["test"] = Flag{State: state}

	counter++
	fmt.Print(store.Flags["test"].State + strconv.Itoa(counter))
}
