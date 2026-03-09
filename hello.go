package main

import (
	"cmp"
	"fmt"
	"sort"
	"strconv"
)

// Note the lack of semicolons, they are optional in Go and are automatically inserted by the compiler at the end of lines,
// but they can be used to separate multiple statements on the same line if needed.
// ====================================================================================================================
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

	switch x {
	case 1:
		fmt.Println("Equals 1")
	case 2:
		fmt.Println("Equals 2")
	default:
		fmt.Println("Not 1 or 2")
	}

// ====================================================================================================================
// maps
	// make is for maps, slices and channels, it allocates and initializes the data structure and returns a reference to it.
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

// ====================================================================================================================
// arrays making an array which most defineitely is NOT an object  
	arrayOfInt := make([]int, 0, 10)
	fmt.Println("Length:", len(arrayOfInt), "Capacity:", cap(arrayOfInt))
	arrayOfInt = append(arrayOfInt, 1, 2, 3)
	fmt.Println("Length:", len(arrayOfInt), "Capacity:", cap(arrayOfInt))

	for i, v := range arrayOfInt {
		fmt.Println("Index:", i, "Value:", v)
	}

	for _, v := range arrayOfInt {
		fmt.Println("Value:", v)
	}

// ====================================================================================================================
// interface
	var greeters []Greeter = []Greeter{&User{Name: "Simon", Age: 68}, &Dog{Name: "Rover", Breed: "Golden Retriever"}}
	for _, g := range greeters {
		g.Greet()
	}

	fmt.Println(Max(1, 2))

	// heap - new initialises to zeroes
	pi := new(int32)	
	fmt.Println("Pi value:", *pi)

	// or
	var pi2 = new(int32)
	fmt.Println("Pi2 value:", *pi2)

	// no delete or free, the garbage collector will take care of it when there are no more references to the allocated memory
} // main

// ====================================================================================================================
// struct
type User struct {
	Name string
	Age  int
}

type Dog struct {
	Name  string
	Breed string
}

// receiver functions, they are like methods in other languages, but they are not associated with the type in the same way as methods in other languages, 
// they are just functions that take a receiver as the first argument. The receiver can be a value or a pointer, and it can be of any type, not just structs. 
// The receiver is specified in parentheses before the function name, and it can be used to access the fields of the struct or to modify the struct if it is a pointer receiver.
func (u *User) Greet() {
	fmt.Println("Hello from", u.Name)
}

func (d *Dog) Greet() {
	fmt.Println("Woof from", d.Name)
}

// Greeter is an interface for greeting implicitly associated with User and Dog since the function has the correct sig.
// ie there is no ccomplementary "implements" keyword, the compiler will check if the function signatures match and if they do,
// it will consider the type to implement the interface.
type Greeter interface {
	Greet()
}

// ====================================================================================================================
// Generic function to get the maximum of two ordered values

func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// ====================================================================================================================