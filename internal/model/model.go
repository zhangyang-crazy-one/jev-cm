package model

type Message struct {
	ID      string
	Role    string
	Kind    string
	Content string
}

type Passage struct {
	SourceID    string
	Text        string
	SHA256      string
	Probability *float64
	Unranked    bool
}
