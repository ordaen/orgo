package types

// Variable is a named value.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Variables is a list of variables.
type Variables []Variable

// IndexOf returns the index of the variable with the name, or -1 when it is not present
func (t Variables) IndexOf(name string) int {
	for k, v := range t {
		if v.Name == name {
			return k
		}
	}
	return -1
}

// Contains returns true if var present
func (t Variables) Contains(name string) bool {
	return t.IndexOf(name) >= 0
}

// Map returns map from Variables
func (t Variables) Map() map[string]any {
	m := map[string]any{}
	for _, v := range t {
		m[v.Name] = v.Value
	}
	return m
}

// StringMap returns map from Variables
func (t Variables) StringMap() map[string]string {
	m := map[string]string{}
	for _, v := range t {
		m[v.Name] = v.Value
	}
	return m
}
