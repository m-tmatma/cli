// Package client provides an abstraction layer for interacting with the
// GitHub Discussions GraphQL API. The DiscussionClient interface defines all
// supported operations and can be replaced with a mock in tests.
package client

import "github.com/cli/cli/v2/internal/ghrepo"

//go:generate moq -rm -out client_mock.go . DiscussionClient

// DiscussionClient defines operations for interacting with the GitHub Discussions API.
type DiscussionClient interface {
	// List returns discussions in a repository matching the given filters.
	List(repo ghrepo.Interface, filters ListFilters, after string, limit int) (*DiscussionListResult, error)
	// Search returns discussions in a repository matching the given search filters.
	Search(repo ghrepo.Interface, filters SearchFilters, after string, limit int) (*DiscussionListResult, error)
	// GetByNumber returns a single discussion by its number.
	GetByNumber(repo ghrepo.Interface, number int32) (*Discussion, error)
	// GetWithComments returns a discussion along with a page of its comments.
	GetWithComments(repo ghrepo.Interface, number int32, commentLimit int, after string, newest bool) (*Discussion, error)
	// GetCommentReplies returns a comment's parent discussion along with a page of the comment's replies.
	GetCommentReplies(host string, commentID string, limit int, after string, newest bool) (*Discussion, error)
	// ListCategories returns the discussion categories available in a repository.
	ListCategories(repo ghrepo.Interface) ([]DiscussionCategory, error)
	// ListLabels returns the labels available in a repository.
	ListLabels(repo ghrepo.Interface) ([]DiscussionLabel, error)
	// Create creates a discussion. The returned discussion may be non-nil even
	// when err is non-nil, indicating a secondary mutation failure (e.g., labels).
	Create(repo ghrepo.Interface, input CreateDiscussionInput) (*Discussion, error)
	// Update updates a discussion. The returned discussion may be non-nil even
	// when err is non-nil, indicating a secondary mutation failure (e.g., labels).
	Update(repo ghrepo.Interface, input UpdateDiscussionInput) (*Discussion, error)
	// AddComment adds a comment or reply to a discussion. If replyToID is
	// non-empty, the comment is created as a reply to that comment.
	AddComment(repo ghrepo.Interface, discussionID, body, replyToID string) (*DiscussionComment, error)
	// UpdateComment updates the body of an existing discussion comment or reply.
	UpdateComment(repo ghrepo.Interface, commentID, body string) (*DiscussionComment, error)
	// DeleteComment deletes a discussion comment or reply.
	DeleteComment(repo ghrepo.Interface, commentID string) error
	// GetComment fetches a single discussion comment by node ID.
	GetComment(host string, commentID string) (*DiscussionComment, error)
	// ResolveCommentNodeID constructs a discussion comment node ID from a
	// repository and a comment database ID (the numeric ID from the URL fragment).
	ResolveCommentNodeID(repo ghrepo.Interface, commentDatabaseID int64) (string, error)
}
