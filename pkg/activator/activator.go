package activator

// Status is an HTTP status code.
type Status int

// Activator provides an active endpoint for a revision or an error and
// status code indicating why it could not.
type Activator interface {
	ActiveEndpoint(RevisionId) (Endpoint, Status, error)
}

// RevisionId is the name and namespace of a revision.
type RevisionId struct {
	name      string
	namespace string
}

func (r RevisionId) string() string {
	return r.namespace + "/" + r.name
}

// Endpoint is an ip, port pair for an active revision.
type Endpoint struct {
	Ip   string
	Port int32
}
