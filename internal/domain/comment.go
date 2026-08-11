package domain

import "time"

type CommentSubject struct {
	Kind string `yaml:"kind" json:"kind"`
	ID   string `yaml:"id" json:"id"`
}

type CommentAuthor struct {
	Name      string `yaml:"name" json:"name"`
	EmailHash string `yaml:"emailHash,omitempty" json:"-"`
	Website   string `yaml:"website,omitempty" json:"website,omitempty"`
}

type Comment struct {
	SchemaVersion   int            `yaml:"schemaVersion" json:"schemaVersion"`
	Kind            string         `yaml:"kind" json:"kind"`
	ID              string         `yaml:"id" json:"id"`
	Subject         CommentSubject `yaml:"subject" json:"subject"`
	ParentID        string         `yaml:"parentId,omitempty" json:"parentId,omitempty"`
	Status          string         `yaml:"status" json:"status"`
	Author          CommentAuthor  `yaml:"author" json:"author"`
	Content         string         `yaml:"content" json:"content"`
	Locale          string         `yaml:"locale" json:"locale"`
	CreatedAt       time.Time      `yaml:"createdAt" json:"createdAt"`
	IPHash          string         `yaml:"ipHash" json:"-"`
	UserAgentFamily string         `yaml:"userAgentFamily" json:"-"`
}
