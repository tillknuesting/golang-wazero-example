package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)

	scanner.Scan() // use for reading line from Stdin
	input := scanner.Text()

	fmt.Println("From Stdin: ", input)

	state := "0"
	if len(os.Args) > 1 {
		state = os.Args[1]
	}

	fmt.Println("From Args: ", state)
}
