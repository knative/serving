### Build

```bash
make local
```

### Run Tests

```bash
make unit-test
make integ-test
```

### Config options

- ChunkSizeInBytes: Size of one chunk in bytes
- DQPServerAddr: IP:Port of Dest QP gRPC server
- LBAddr: IP:Port of load balancer server
- DstServerAddr: IP:Port of Dest function gRPC server
- SQPServerAddr: IP:Port of Src QP server
- NumberOfBuffers: Number of buffer channels to create
- BufferSize: Number of chunks to buffer inside sQP and dQP
- StAndFwBufferSize:  Total size of buffer in store and forward routing
- Routing : Routing type [Store&Forward, CutThrough]
- TracingEnabled : Enable tracing using open-telemetry [true,false]
