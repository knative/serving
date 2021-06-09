module queue

go 1.16

replace (
	XDTprototype/dqp => ./prototype/dQP
	XDTprototype/proto/crossXDT => ./prototype/proto/crossXDT
	XDTprototype/proto/downXDT => ./prototype/proto/downXDT
	XDTprototype/proto/fnInvocation => ./prototype/proto/fnInvocation
	XDTprototype/proto/upXDT => ./prototype/proto/upXDT
	XDTprototype/sqp => ./prototype/sQP
	XDTprototype/transport => ./prototype/transport
	XDTprototype/utils => ./prototype/utils
)

require (
	XDTprototype/dqp v0.0.0-00010101000000-000000000000
	XDTprototype/sqp v0.0.0-00010101000000-000000000000
	XDTprototype/utils v0.0.0-00010101000000-000000000000
	github.com/kelseyhightower/envconfig v1.4.0
	go.opencensus.io v0.23.0
	go.uber.org/atomic v1.7.0
	go.uber.org/automaxprocs v1.4.0
	go.uber.org/zap v1.17.0
	k8s.io/apimachinery v0.21.1
	knative.dev/networking v0.0.0-20210609003242-c743329dacb9
	knative.dev/pkg v0.0.0-20210608193741-f19eef192438
	knative.dev/serving v0.23.0
)
