package activator

// Status is an HTTP status code.
type Status int

// Activator provides an active endpoint for a revision or an error and
// status code indicating why it could not.
type Activator interface {
	ActiveEndpoint(namespace, name string) (Endpoint, Status, error)
	Shutdown()
}

type revisionId struct {
	namespace string
	name      string
}

// Endpoint is an ip, port pair for an active revision.
type Endpoint struct {
	Ip   string
	Port int32
}
