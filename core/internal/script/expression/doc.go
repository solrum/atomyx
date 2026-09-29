// Package expression implements the test-script expression language:
// tokenizing, parsing, and evaluating boolean and arithmetic expressions
// over a set of named variables (used by assertExpr, branch and handle
// conditions, and extract/capture patterns).
//
// Every error message produced by this package, and the exact wording of
// every value description embedded in an error message, is a behavioral
// contract: assertExpr and branch-condition failure reasons surface this
// text verbatim to the person or agent authoring the test script, and a
// second implementation of this same language reproduces this wording
// byte for byte so the two stay interchangeable. Changing a message
// string is a breaking change to that contract, not a wording cleanup.
package expression
