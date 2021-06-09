module sqp

go 1.16

replace (
	github.com/ease-lab/XDTprototype/utils => ../utils
	github.com/ease-lab/XDTprototype/proto/crossXDT => ../proto/crossXDT
	github.com/ease-lab/XDTprototype/proto/upXDT => ../proto/upXDT
	github.com/ease-lab/XDTprototype/transport => ../transport
)

require (
	github.com/ease-lab/XDTprototype/utils v0.0.0-00010101000000-000000000000
	github.com/ease-lab/XDTprototype/proto/crossXDT v0.0.0-00010101000000-000000000000
	github.com/ease-lab/XDTprototype/proto/upXDT v0.0.0-00010101000000-000000000000
	github.com/ease-lab/XDTprototype/transport v0.0.0-00010101000000-000000000000
	github.com/sirupsen/logrus v1.8.1
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.20.0
	google.golang.org/grpc v1.37.0
)
