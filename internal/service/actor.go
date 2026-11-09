package service

// Actor is who is making a change. The vocabulary is deliberately two kinds
// and no more (D-13): a human at the local machine, or a service acting on the
// operator's behalf. Anything else wanting a name gets a new kind and a
// migration story, not a free-form string.
type Actor struct {
	Kind string
	Name string
}

const (
	// KindLocal is a human invoking the CLI on the host (including via sudo,
	// where the name is the invoking user, not root).
	KindLocal = "local"

	// KindService is a daemon acting for the operator.
	KindService = "service"
)

// String renders the actor in the single form stored in updated_by/actor.
func (a Actor) String() string {
	return a.Kind + ":" + a.Name
}
