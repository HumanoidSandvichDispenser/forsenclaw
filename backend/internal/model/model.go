package model

// Item is a minimal domain model used by the example server.
type Item struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}
