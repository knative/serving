module dqp

go 1.16

replace (
	github.com/ease-lab/XDTprototype/utils => ../utils
	github.com/ease-lab/XDTprototype/proto/crossXDT => ../proto/crossXDT
	github.com/ease-lab/XDTprototype/proto/downXDT => ../proto/downXDT
	github.com/ease-lab/XDTprototype/proto/fnInvocation => ../proto/fnInvocation
	github.com/ease-lab/XDTprototype/transport => ../transport
)

require (
	github.com/ease-lab/XDTprototype/utils v0.0.0-00010101000000-000000000000
	github.com/ease-lab/XDTprototype/proto/crossXDT v0.0.0-00010101000000-000000000000
	github.com/ease-lab/XDTprototype/proto/downXDT v0.0.0-00010101000000-000000000000
	github.com/ease-lab/XDTprototype/proto/fnInvocation v0.0.0-00010101000000-000000000000
	github.com/ease-lab/XDTprototype/transport v0.0.0-00010101000000-000000000000
	github.com/sirupsen/logrus v1.8.1
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.20.0
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	google.golang.org/grpc v1.37.0
)
