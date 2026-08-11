package domain

import "time"

type MediaAsset struct {
	SchemaVersion int       `yaml:"schemaVersion" json:"schemaVersion"`
	Kind          string    `yaml:"kind" json:"kind"`
	ID            string    `yaml:"id" json:"id"`
	Filename      string    `yaml:"filename" json:"filename"`
	OriginalName  string    `yaml:"originalName" json:"originalName"`
	MIMEType      string    `yaml:"mimeType" json:"mimeType"`
	Size          int64     `yaml:"size" json:"size"`
	URL           string    `yaml:"url" json:"url"`
	CreatedAt     time.Time `yaml:"createdAt" json:"createdAt"`
}
