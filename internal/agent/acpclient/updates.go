package acpclient

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"coordica/internal/process"
)

const updateLimit = 32
const updateByteLimit = 16 << 10

var ErrUpdatePosition = errors.New("invalid ACP local update release position")

// UpdateFact is metadata only. Local sequence is not a native event ID, text
// byte count is not output content, and tool status is not Turn completion.
type UpdateFact struct {
	Sequence      int64
	Kind          string
	TextBytes     int64
	ToolCallID    string
	State         string
	ConfigVersion uint64 // Local observation order, never server CAS/effective settings.
}

type UpdateSnapshot struct {
	Epoch                                             string
	Evidence                                          string
	SessionID                                         string
	Facts                                             []UpdateFact
	ObservedThrough, AcceptedThrough, ReleasedThrough int64
	Gap                                               bool
	Reason                                            string
	Ended                                             bool
	Process                                           process.Result
}

type updateCapture struct {
	mu                                    sync.Mutex
	epoch, session                        string
	facts                                 []UpdateFact
	bytes                                 int
	observed, accepted, released, offered int64
	gap, ended, intentional               bool
	reason                                string
	process                               process.Result
}

func newUpdateCapture() (*updateCapture, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	return &updateCapture{epoch: "local_" + hex.EncodeToString(nonce[:])}, nil
}

func (c *updateCapture) bind(id string) { c.mu.Lock(); defer c.mu.Unlock(); c.session = id }
func (c *updateCapture) stopping()      { c.mu.Lock(); defer c.mu.Unlock(); c.intentional = true }
func (c *updateCapture) fault(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gap = true
	if c.reason == "" {
		c.reason = reason
	}
}
func (c *updateCapture) finish(status process.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ended, c.process = true, status
	// Closing can leave unread notifications; no final native replay/high-water
	// proof exists in this slice, even for an intentional local Close.
	c.gap = true
	if c.reason == "" {
		if c.intentional {
			c.reason = "closed_unconfirmed"
		} else {
			c.reason = "transport_lost"
		}
	}
}

func (c *updateCapture) append(fact UpdateFact) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observed++
	weight := 64 + len(fact.Kind) + len(fact.ToolCallID) + len(fact.State)
	if len(c.facts) >= updateLimit || c.bytes+weight > updateByteLimit {
		c.gap = true
		c.reason = "backpressure"
		return ErrBackpressure
	}
	fact.Sequence = c.observed
	c.facts = append(c.facts, fact)
	c.bytes += weight
	c.accepted = c.observed
	return nil
}

func (s Session) capture() (*updateCapture, error) {
	if s.peer == nil {
		return nil, ErrProtocol
	}
	p := s.peer
	p.mu.Lock()
	valid := p.sessionReady && p.sessionID == s.id && p.directory == s.directory && p.updates.epoch == s.epoch
	p.mu.Unlock()
	if !valid {
		return nil, ErrProtocol
	}
	return p.updates, nil
}

// Updates returns a bounded defensive snapshot, including sticky diagnostics
// after failure. It does not wait for native I/O, a consumer or a database.
func (s Session) Updates() (UpdateSnapshot, error) {
	c, err := s.capture()
	if err != nil {
		return UpdateSnapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offered = c.accepted
	return UpdateSnapshot{Epoch: c.epoch, Evidence: "local_observed", SessionID: c.session, Facts: append([]UpdateFact{}, c.facts...),
		ObservedThrough: c.observed, AcceptedThrough: c.accepted, ReleasedThrough: c.released, Gap: c.gap, Reason: c.reason, Ended: c.ended, Process: c.process}, nil
}

// ReleaseUpdates advances only the local offered prefix. It sends no native
// ACK and cannot clear a gap, repair lost history, or resume a failed Peer.
func (s Session) ReleaseUpdates(through int64) error {
	c, err := s.capture()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if through < c.released || through > c.offered {
		return ErrUpdatePosition
	}
	count := 0
	for count < len(c.facts) && c.facts[count].Sequence <= through {
		c.bytes -= 64 + len(c.facts[count].Kind) + len(c.facts[count].ToolCallID) + len(c.facts[count].State)
		count++
	}
	c.facts = append([]UpdateFact{}, c.facts[count:]...)
	c.released = through
	return nil
}
