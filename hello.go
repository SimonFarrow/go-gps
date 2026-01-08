package main

import (
	"cmp"
	"fmt"
	"sort"
	"strconv"
)

func main() {
	for i := range 10 {
		fmt.Println("Hello, World! " + strconv.Itoa(i))
	}

	x := 2

	if x == 1 {
		fmt.Println("Equals 1")
	} else {
		fmt.Println("Does not equal 1")
	}

	m := make(map[string]int)
	m["one"] = 1
	m["two"] = 2
	m["three"] = 3

	for k, v := range m {
		fmt.Println("Key:", k, "Value:", v)
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	for _, k := range keys {
		fmt.Println("Sorted Key:", k, "Value:", m[k])
	}

	switch x {
	case 1:
		fmt.Println("Equals 1")
	case 2:
		fmt.Println("Equals 2")
	default:
		fmt.Println("Not 1 or 2")
	}

	var greeters []Greeter = []Greeter{&User{Name: "Simon", Age: 68}, &Dog{Name: "Rover", Breed: "Golden Retriever"}}
	for _, g := range greeters {
		g.Greet()
	}

	fmt.Println(Max(1, 2))
}

type User struct {
	Name string
	Age  int
}

type Dog struct {
	Name  string
	Breed string
}

func (u *User) Greet() {
	fmt.Println("Hello from", u.Name)
}

func (d *Dog) Greet() {
	fmt.Println("Woof from", d.Name)
}

// Greeter is an interface for greeting immplicitly associated with User and Dog since the function has the correct sig.
type Greeter interface {
	Greet()
}

// Generic function to get the maximum of two ordered values
func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}
