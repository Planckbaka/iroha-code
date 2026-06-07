package tui

// HistoryManager manages command history for CLI prompts
type HistoryManager struct {
	Items []string
	Index int
}

func NewHistoryManager() *HistoryManager {
	return &HistoryManager{
		Items: make([]string, 0),
		Index: -1,
	}
}

func (hm *HistoryManager) Add(item string) {
	if item == "" {
		return
	}
	// Avoid duplicate consecutive items
	if len(hm.Items) > 0 && hm.Items[len(hm.Items)-1] == item {
		hm.Index = len(hm.Items)
		return
	}
	hm.Items = append(hm.Items, item)
	hm.Index = len(hm.Items)
}

func (hm *HistoryManager) Up() string {
	if len(hm.Items) == 0 {
		return ""
	}
	if hm.Index > 0 {
		hm.Index--
	}
	return hm.Items[hm.Index]
}

func (hm *HistoryManager) Down() string {
	if len(hm.Items) == 0 {
		return ""
	}
	if hm.Index < len(hm.Items)-1 {
		hm.Index++
		return hm.Items[hm.Index]
	}
	hm.Index = len(hm.Items)
	return ""
}
