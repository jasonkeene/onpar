package onpar

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
)

// Stores the state of the specs and groups
type Onpar struct {
	current   *level
	callCount int
}

// Creates a new Onpar test suite
func New() *Onpar {
	return NewWithCallCount(1)
}

func NewWithCallCount(count int) *Onpar {
	return &Onpar{
		current:   new(level),
		callCount: count,
	}
}

// Spec is a test that runs in parallel with other specs. The provided function
// takes the `testing.T` for test assertions and any arguments the `BeforeEach()`
// returns.
func (o *Onpar) Spec(name string, f interface{}) {
	_, fileName, lineNumber, _ := runtime.Caller(o.callCount)
	v := reflect.ValueOf(f)
	spec := specInfo{
		name:       name,
		f:          &v,
		ft:         reflect.TypeOf(f),
		fileName:   fileName,
		lineNumber: lineNumber,
	}
	o.current.specs = append(o.current.specs, spec)
}

// Group is used to gather and categorize specs. Each group can have a single
// `BeforeEach()` and `AfterEach()`.
func (o *Onpar) Group(name string, f func()) {
	newLevel := &level{
		name:   name,
		parent: o.current,
	}

	o.current.children = append(o.current.children, newLevel)

	oldLevel := o.current
	o.current = newLevel
	f()
	o.current = oldLevel
}

// BeforeEach is used for any setup that may be required for the specs.
// Each argument returned will be required to be received by following specs.
// Outer BeforeEaches are invoked before inner ones.
func (o *Onpar) BeforeEach(f interface{}) {
	if o.current.before != nil {
		panic(fmt.Sprintf("Level '%s' already has a registered BeforeEach", o.current.name))
	}
	_, fileName, lineNumber, _ := runtime.Caller(o.callCount)

	v := reflect.ValueOf(f)
	o.current.before = &specInfo{
		f:          &v,
		ft:         reflect.TypeOf(f),
		fileName:   fileName,
		lineNumber: lineNumber,
	}
}

// AfterEach is used to cleanup anything from the specs or BeforeEaches.
// The function takes arguments the same as specs. Inner AfterEaches are invoked
// before outer ones.
func (o *Onpar) AfterEach(f interface{}) {
	if o.current.after != nil {
		panic(fmt.Sprintf("Level '%s' already has a registered AfterEach", o.current.name))
	}

	_, fileName, lineNumber, _ := runtime.Caller(o.callCount)

	v := reflect.ValueOf(f)
	o.current.after = &specInfo{
		f:          &v,
		ft:         reflect.TypeOf(f),
		fileName:   fileName,
		lineNumber: lineNumber,
	}
}

// Run is used to initiate the tests.
func (o *Onpar) Run(t *testing.T) {
	traverse(o.current, func(l *level) {
		for _, spec := range l.specs {
			spec.invoke(t, l)
		}
	})
}

type level struct {
	before, after *specInfo
	name          string
	specs         []specInfo

	children []*level
	parent   *level

	beforeEachArgs []reflect.Value
}

type specInfo struct {
	name string
	f    *reflect.Value
	ft   reflect.Type

	fileName   string
	lineNumber int
}

func (s specInfo) invoke(t *testing.T, l *level) {
	desc := buildDesc(l, s)
	t.Run(desc, func(tt *testing.T) {
		tt.Parallel()

		args, levelArgs := invokeBeforeEach(tt, l)

		verifySpecCall(s, args)

		s.f.Call(args)

		invokeAfterEach(tt, l, levelArgs)
	})
}

func verifySpecCall(s specInfo, args []reflect.Value) {
	if s.ft.NumOut() != 0 {
		panic("Spec functions must not return anything")
	}

	verifyCall("Spec", s, args)
}

func verifyCall(name string, s specInfo, args []reflect.Value) {
	if s.ft.NumIn() != len(args) {
		argStr := buildReadableArgs(args)
		panic(
			fmt.Sprintf("Expected %s func (%s:%d) to take arguments: %v",
				name, s.fileName, s.lineNumber, argStr),
		)
	}
}

func buildReadableArgs(args []reflect.Value) string {
	var result string
	for _, arg := range args {
		result = fmt.Sprintf("%s, %s", result, arg.Type().String())
	}
	return result[1:]
}

func invokeBeforeEach(tt *testing.T, l *level) ([]reflect.Value, map[*level][]reflect.Value) {
	args := []reflect.Value{
		reflect.ValueOf(tt),
	}
	levelArgs := make(map[*level][]reflect.Value)

	type beforeEachInfo struct {
		s *specInfo
		l *level
	}
	var beforeEaches []beforeEachInfo

	rTraverse(l, func(ll *level) {
		if ll.before != nil {
			beforeEaches = append(beforeEaches, beforeEachInfo{
				s: ll.before,
				l: ll,
			})
		}
	})

	for i := len(beforeEaches) - 1; i >= 0; i-- {
		be := beforeEaches[i]

		verifyCall("BeforeEach", *be.s, args)

		args = append(args, be.s.f.Call(args)...)
		levelArgs[be.l] = args
	}

	return args, levelArgs
}

func invokeAfterEach(tt *testing.T, l *level, levelArgs map[*level][]reflect.Value) {
	rTraverse(l, func(ll *level) {
		beforeEachArgs := levelArgs[ll]
		if beforeEachArgs == nil {
			beforeEachArgs = []reflect.Value{
				reflect.ValueOf(tt),
			}
		}

		if ll.after != nil {
			verifyCall("AfterEach", *ll.after, beforeEachArgs)
			ll.after.f.Call(beforeEachArgs)
		}
	})
}

func buildDesc(l *level, i specInfo) string {
	desc := i.name
	rTraverse(l, func(ll *level) {
		desc = fmt.Sprintf("%s/%s", ll.name, desc)
	})

	return desc
}

func traverse(l *level, f func(*level)) {
	if l == nil {
		return
	}

	f(l)

	for _, child := range l.children {
		traverse(child, f)
	}
}

func rTraverse(l *level, f func(*level)) {
	if l == nil {
		return
	}

	f(l)

	rTraverse(l.parent, f)
}
