package main

import (
	"fmt"
	"os"
)

type Flag struct {
	State string `json:"state"`
}

type Store struct {
	Flags map[string]Flag `json:"flags"`
}

func main() {
	state := "ENABLED"
	if len(os.Args) > 1 {
		state = os.Args[1]
	}

	store := Store{Flags: make(map[string]Flag)}

	store.Flags["test"] = Flag{State: state}

	fmt.Print(store.Flags["test"].State)
}
