package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/gorilla/mux"

	"flag"
)

var resource string

func main() {
	flag.Parse()
	router := mux.NewRouter().StrictSlash(true)

	resource = os.Getenv("RESOURCE")
	if resource == "" {
		resource = "NOT SPECIFIED"
	}

	root := "/" + resource
	path := root + "/{stockId}"

	router.HandleFunc("/", Index)
	router.HandleFunc(root, StockIndex)
	router.HandleFunc(path, StockPrice)

	log.Fatal(http.ListenAndServe(":8080", router))
}

func Index(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Welcome to the %s app! \n", resource)
}

func StockIndex(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "%s ticker not found!, require /%s/{ticker}\n", resource, resource)
}

func StockPrice(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	stockId := vars["stockId"]

	url := url.URL{
		Scheme: "https",
		Host:   "api.iextrading.com",
		Path:   "/1.0/stock/" + stockId + "/price",
	}

	log.Print(url)

	resp, err := http.Get(url.String())
	if err != nil {
		fmt.Fprintf(w, "%s not found for ticker : %s \n", resource, stockId)
		return
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	fmt.Fprintf(w, "%s price for ticker %s is %s\n", resource, stockId, string(body))
}
