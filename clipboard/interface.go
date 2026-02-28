package clipboard

type ClipboardInterface interface {
	GetText() (string, error)
	SetText(text string) error
	Watch()
	Stop()
}
