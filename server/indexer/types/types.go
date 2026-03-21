package types

type DocType int

const (
	Web DocType = iota
	Local
	Media
)

var DocTypeNames = map[string]DocType{
	"web":   Web,
	"file":  Local,
	"local": Local,
	"media": Media,
}
