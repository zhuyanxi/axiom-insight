package planner

import (
	"context"
	"sync"
	"testing"
)

// TestPlanConcurrentSameDocument: planning the same read-only IR from
// many goroutines with one shared Planner must not race (run with -race).
func TestPlanConcurrentSameDocument(t *testing.T) {
	planner, _, _, _ := newTestPlanner()
	document := testDocument()
	policy := defaultPolicy()

	const workers = 8
	var wait sync.WaitGroup
	wait.Add(workers)
	failures := make(chan error, workers)
	for range workers {
		go func() {
			defer wait.Done()
			plan, _, err := planner.Plan(context.Background(), document, policy)
			if err != nil {
				failures <- err
				return
			}
			if plan == nil {
				failures <- errNilPlan
			}
		}()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("concurrent planning failed: %v", err)
	}
}

// TestPlanConcurrentDistinctDocuments: one Planner planning different
// documents concurrently must not share state.
func TestPlanConcurrentDistinctDocuments(t *testing.T) {
	planner, _, _, _ := newTestPlanner()
	policy := defaultPolicy()

	const workers = 8
	var wait sync.WaitGroup
	wait.Add(workers)
	failures := make(chan error, workers)
	for index := range workers {
		go func(index int) {
			defer wait.Done()
			document := testDocument()
			document.Service.Name = "service-" + itoa(index)
			plan, _, err := planner.Plan(context.Background(), document, policy)
			if err != nil {
				failures <- err
				return
			}
			if plan == nil || plan.ServiceName != "service-"+itoa(index) {
				failures <- errWrongService
			}
		}(index)
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("concurrent planning failed: %v", err)
	}
}

var (
	errNilPlan      = errTest("plan is nil")
	errWrongService = errTest("plan carries another service's name")
)

type errTest string

func (err errTest) Error() string { return string(err) }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [32]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}
