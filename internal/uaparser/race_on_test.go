//go:build race

package uaparser

// raceFactor stretches the parse timeout in tests: the race detector slows the VM down.
const raceFactor = 40
