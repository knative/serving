mod_file = {
    "go.mod": """module {}\ngo 1.17"""
}
docker_file = {
    "Dockerfile": """
    ## We specify the base image we need for our
    ## go application
    FROM golang:1.12.1-alpine3.9
    RUN apk add build-base
    ## We create an /app directory within our
    ## image that will hold our application source
    ## files
    RUN mkdir /app
    ## We copy everything in the root directory
    ## into our /app directory
    ADD . /app
    ## We specify that we now wish to execute 
    ## any further commands inside our /app
    ## directory
    WORKDIR /app
    ## we run go build to compile the binary
    ## executable of our Go program
    RUN go build -o main .
    ## Our start command which kicks off
    ## our newly created binary executable
    CMD ["/app/main"]
"""
}

application_file = '''
    package main

    import (
        "fmt"
        "math"
        "net/http"
        "runtime/debug"
        "time"
    )

    func hello(w http.ResponseWriter, req *http.Request) {{
        start_time := time.Now()
        given_time := {}
        const given_memory = {} * 1024 * 1024
        time_in_milliseconds := math.Floor(given_time)
        time_in_nanoseconds := (given_time - time_in_milliseconds) * 1000000
        actual_exec_time := time.Duration(time_in_milliseconds)*time.Millisecond + time.Duration(time_in_nanoseconds)*time.Nanosecond

        mem_alloc_start_time := time.Now()
        var memory_footprint_ds [int(given_memory)]byte
        mem_alloc_end_time := time.Now()
        fmt.Printf("\\nmemory alloc time: %v\\n", mem_alloc_end_time.Sub(mem_alloc_start_time))
        curr_time := time.Now()
        index := 0
        for curr_time.Sub(mem_alloc_end_time) < actual_exec_time {{
            memory_footprint_ds[index] = 1
            index += 1
            curr_time = time.Now()
        }}

        end_time_before_sleep := time.Now()
        fmt.Printf("\\nloop time: %v\\n", end_time_before_sleep.Sub(mem_alloc_end_time))
        overhead_exec_so_far := end_time_before_sleep.Sub(start_time)

        if overhead_exec_so_far < actual_exec_time {{
            time.Sleep(actual_exec_time - overhead_exec_so_far)
        }}
        end_time := time.Now()
        fmt.Printf("\\narr size %vstart: %v end: %v duration: %v\\n", len(memory_footprint_ds), start_time.UTC(), end_time.UTC(), end_time.Sub(start_time))
        debug.FreeOSMemory()
    }}

    func main() {{

        http.HandleFunc("/hello", hello)
        http.ListenAndServe(":8080", nil)
    }}

'''