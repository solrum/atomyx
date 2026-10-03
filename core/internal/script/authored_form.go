package script

import (
	"reflect"
	"strconv"
	"strings"
)

// placeholderStandInBase is the first of the numbers that stand in for
// `${name}` placeholders while a step is normalized. A stand-in is a
// run of digits, so it is accepted by a string, a number or an
// identifier field alike.
const placeholderStandInBase = 739100000000

// maxPlaceholderStandIns bounds how many distinct names one step may
// use before its written form is given up.
const maxPlaceholderStandIns = 1000

// writtenForm returns the step at index as the author wrote it, with
// `${name}` placeholders intact, or nil when it cannot be rebuilt. A
// placeholder in a field that rejects the literal text, such as a
// number or an identifier, is replaced by a digit stand-in for
// normalization and restored afterwards. A placeholder that survives as
// a number cannot be restored, so the step reports nil.
func writtenForm(raw any, index int) ScriptStep {
	if step, err := normalizeStep(raw, index); err == nil {
		return step
	}
	table := &standInTable{}
	step, err := normalizeStep(expandPlaceholders(raw, table.lookup), index)
	if err != nil || table.overflow {
		return nil
	}
	if !table.restore(reflect.ValueOf(&step).Elem()) {
		return nil
	}
	return step
}

type standInTable struct {
	names    []string
	overflow bool
}

func (t *standInTable) lookup(name string) (string, bool) {
	for i, n := range t.names {
		if n == name {
			return strconv.Itoa(placeholderStandInBase + i), true
		}
	}
	if len(t.names) >= maxPlaceholderStandIns {
		t.overflow = true
	}
	t.names = append(t.names, name)
	return strconv.Itoa(placeholderStandInBase + len(t.names) - 1), true
}

func (t *standInTable) isNumberStandIn(n float64) bool {
	return n >= placeholderStandInBase && n < placeholderStandInBase+float64(len(t.names))
}

func (t *standInTable) restoreText(s string) string {
	for i, name := range t.names {
		s = strings.ReplaceAll(s, strconv.Itoa(placeholderStandInBase+i), "${"+name+"}")
	}
	return s
}

// restore rewrites every string reachable from v, replacing stand-ins
// with their placeholders. It reports false when a number field holds a
// stand-in, because a number cannot carry a placeholder.
func (t *standInTable) restore(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return true
		}
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		ok := t.restore(inner)
		v.Set(inner)
		return ok
	case reflect.Pointer:
		return v.IsNil() || t.restore(v.Elem())
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(OrderedMap{}) {
			return t.restoreOrderedMap(v.Addr().Interface().(*OrderedMap))
		}
		ok := true
		for i := 0; i < v.NumField(); i++ {
			if f := v.Field(i); f.CanSet() && !t.restore(f) {
				ok = false
			}
		}
		return ok
	case reflect.String:
		v.SetString(t.restoreText(v.String()))
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return !t.isNumberStandIn(float64(v.Int()))
	case reflect.Float32, reflect.Float64:
		return !t.isNumberStandIn(v.Float())
	case reflect.Slice:
		ok := true
		for i := 0; i < v.Len(); i++ {
			if !t.restore(v.Index(i)) {
				ok = false
			}
		}
		return ok
	case reflect.Map:
		ok := true
		for _, key := range v.MapKeys() {
			entry := reflect.New(v.Type().Elem()).Elem()
			entry.Set(v.MapIndex(key))
			if !t.restore(entry) {
				ok = false
			}
			v.SetMapIndex(key, entry)
		}
		return ok
	}
	return true
}

func (t *standInTable) restoreOrderedMap(m *OrderedMap) bool {
	ok := true
	for _, key := range m.keys {
		holder := new(any)
		*holder = m.values[key]
		if !t.restore(reflect.ValueOf(holder).Elem()) {
			ok = false
		}
		m.values[key] = *holder
	}
	return ok
}
