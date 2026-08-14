// Package publicstate coordinates public response admission across durable
// resume and discovery generations.
package publicstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Representation string

const (
	RepresentationJSON     Representation = "json"
	RepresentationPhoto    Representation = "photo"
	RepresentationHTML     Representation = "html"
	RepresentationMarkdown Representation = "markdown"
	RepresentationSitemap  Representation = "sitemap"
	RepresentationRobots   Representation = "robots"
	RepresentationLLMS     Representation = "llms"
)

type TransitionClass uint8

const (
	NonDraining TransitionClass = iota
	Revoking
)

type ResumeTarget struct {
	ID               uuid.UUID
	ExpectedRevision int64
	Class            TransitionClass
}

type Plan struct {
	DiscoveryGeneration *int64
	Resumes             []ResumeTarget
}

type CommittedState struct {
	DiscoveryGeneration *int64
	ResumeRevisions     map[uuid.UUID]int64
	RetiredResumes      []uuid.UUID
}

type RecoveryDisposition uint8

const (
	RecoveryCommitted RecoveryDisposition = iota + 1
	RecoveryNotCommitted
)

type RecoveryProof struct {
	Disposition RecoveryDisposition
	State       CommittedState
}

type RecoveryResolver interface {
	Resolve(context.Context) (RecoveryProof, error)
}

var ErrAdmissionClosed = errors.New("publicstate: admission closed")

type GenerationMismatchError struct {
	Expected int64
	Actual   int64
}

func (e *GenerationMismatchError) Error() string {
	return fmt.Sprintf("publicstate: generation mismatch: expected %d, actual %d", e.Expected, e.Actual)
}

type DrainTimeoutError struct {
	Deadline time.Time
}

func (e *DrainTimeoutError) Error() string {
	return fmt.Sprintf("publicstate: drain timed out at %s", e.Deadline.UTC().Format(time.RFC3339Nano))
}

type RecoveryUnresolvedError struct {
	Cause error
}

func (e *RecoveryUnresolvedError) Error() string {
	if e.Cause == nil {
		return "publicstate: recovery unresolved"
	}
	return "publicstate: recovery unresolved: " + e.Cause.Error()
}

func (e *RecoveryUnresolvedError) Unwrap() error { return e.Cause }

type CoordinatorConfig struct {
	DiscoveryGeneration int64
	Now                 func() time.Time
}

type Coordinator struct {
	mu      sync.Mutex
	now     func() time.Time
	global  *fence
	resumes map[uuid.UUID]*fence
	metrics fenceMetrics
}

func NewCoordinator(config CoordinatorConfig) (*Coordinator, error) {
	if config.DiscoveryGeneration <= 0 {
		return nil, errors.New("publicstate: discovery generation must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Coordinator{
		now:     config.Now,
		global:  newFence(config.DiscoveryGeneration),
		resumes: make(map[uuid.UUID]*fence),
	}, nil
}

func (c *Coordinator) AcquireResume(ctx context.Context, id uuid.UUID, expected int64, rep Representation) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if expected <= 0 {
		return nil, errors.New("publicstate: resume revision must be positive")
	}
	c.mu.Lock()
	f := c.resumes[id]
	if f == nil {
		f = newFence(expected)
		c.resumes[id] = f
	}
	c.mu.Unlock()
	return f.acquire(ctx, expected, rep, &c.metrics)
}

func (c *Coordinator) AcquireDiscovery(ctx context.Context, expected int64, rep Representation) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if expected <= 0 {
		return nil, errors.New("publicstate: discovery generation must be positive")
	}
	return c.global.acquire(ctx, expected, rep, &c.metrics)
}

func (c *Coordinator) Ready() error {
	c.mu.Lock()
	fences := make([]*fence, 0, len(c.resumes)+1)
	fences = append(fences, c.global)
	for _, f := range c.resumes {
		fences = append(fences, f)
	}
	c.mu.Unlock()
	for _, f := range fences {
		if err := f.ready(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) Begin(ctx context.Context, plan Plan) (*Transition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan.DiscoveryGeneration == nil && len(plan.Resumes) == 0 {
		return nil, errors.New("publicstate: transition plan is empty")
	}
	targets := make([]transitionTarget, 0, len(plan.Resumes)+1)
	if plan.DiscoveryGeneration != nil {
		if *plan.DiscoveryGeneration <= 0 {
			return nil, errors.New("publicstate: discovery generation must be positive")
		}
		targets = append(targets, transitionTarget{
			fence: c.global, expected: *plan.DiscoveryGeneration, global: true, class: Revoking,
		})
	}

	c.mu.Lock()
	for _, resume := range plan.Resumes {
		if resume.ExpectedRevision <= 0 {
			c.mu.Unlock()
			return nil, errors.New("publicstate: resume revision must be positive")
		}
		if resume.Class != NonDraining && resume.Class != Revoking {
			c.mu.Unlock()
			return nil, errors.New("publicstate: invalid transition class")
		}
		f := c.resumes[resume.ID]
		if f == nil {
			f = newFence(resume.ExpectedRevision)
			c.resumes[resume.ID] = f
		}
		targets = append(targets, transitionTarget{
			fence: f, expected: resume.ExpectedRevision, id: resume.ID, class: resume.Class,
		})
	}
	c.mu.Unlock()
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].global != targets[j].global {
			return targets[i].global
		}
		return bytes.Compare(targets[i].id[:], targets[j].id[:]) < 0
	})
	for i := 1; i < len(targets); i++ {
		if !targets[i].global && targets[i].id == targets[i-1].id {
			return nil, errors.New("publicstate: duplicate resume transition")
		}
	}
	for i := range targets {
		if err := targets[i].fence.claim(ctx, targets[i].expected); err != nil {
			for claimed := range targets[:i] {
				targets[claimed].fence.releaseOwner()
			}
			return nil, err
		}
	}
	return &Transition{coordinator: c, targets: targets}, nil
}
