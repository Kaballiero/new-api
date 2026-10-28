// Package quota preserves shared cache operation outcomes without assigning storage ownership.
package quota

type Result int

const (
	Insufficient Result = iota
	OK
	Miss
)

func FromLua(result int, err error) (Result, error) {
	if err != nil {
		return Miss, err
	}
	switch result {
	case 1:
		return OK, nil
	case 0:
		return Insufficient, nil
	default:
		return Miss, nil
	}
}
