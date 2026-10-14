package runner

import "sync"

// SecretSink receives the value of every variable a run marks secret.
type SecretSink interface {
	Add(value string)
}

// feedSecrets hands the sink the value of every variable that starts the
// run tagged secret.
func (r *ScriptRunner) feedSecrets(rs *runState) {
	if r.deps.SecretSink == nil {
		return
	}
	for name := range rs.secrets {
		if value, ok := rs.variables[name]; ok {
			r.deps.SecretSink.Add(scriptValueToEnvString(value))
		}
	}
}

// RunSecrets collects the secret values of one run folder for as long as
// the folder is written. The set only grows, so text rendered after a
// value is added is masked and text written earlier is left as it was. It
// is safe for concurrent use.
type RunSecrets struct {
	mu       sync.Mutex
	values   map[string]struct{}
	redactor *Redactor
}

var _ SecretSink = (*RunSecrets)(nil)

// NewRunSecrets returns an empty set.
func NewRunSecrets() *RunSecrets {
	return &RunSecrets{values: map[string]struct{}{}}
}

// Add records value as secret. An empty value is ignored.
func (s *RunSecrets) Add(value string) {
	if value == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, known := s.values[value]; known {
		return
	}
	s.values[value] = struct{}{}
	s.redactor = nil
}

// AddExternal records the value of every env entry that is not named
// committed. A host calls it before it parses a script, so a secret quoted
// in a parse error is masked too.
func (s *RunSecrets) AddExternal(env map[string]string, committed map[string]bool) {
	for name, value := range env {
		if !committed[name] {
			s.Add(value)
		}
	}
}

// String returns text with every secret recorded so far replaced by the
// mask.
func (s *RunSecrets) String(text string) string {
	return s.current().String(text)
}

func (s *RunSecrets) current() *Redactor {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.redactor == nil {
		texts := make([]string, 0, len(s.values))
		for value := range s.values {
			texts = append(texts, value)
		}
		s.redactor = newRedactor(texts)
	}
	return s.redactor
}
