package comment

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/pkg/cmd/discussion/client"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdComment(t *testing.T) {
	tests := []struct {
		name         string
		args         string
		isTTY        bool
		wantOpts     CommentOptions
		wantBaseRepo ghrepo.Interface
		wantErr      string
	}{
		{
			name:  "add comment with body",
			args:  "123 --body 'Hello world'",
			isTTY: true,
			wantOpts: CommentOptions{
				DiscussionNumber: 123,
				Body:             "Hello world",
			},
		},
		{
			name:  "add reply",
			args:  "123 --reply-to DC_abc --body 'Reply text'",
			isTTY: true,
			wantOpts: CommentOptions{
				DiscussionNumber: 123,
				Body:             "Reply text",
				ReplyTo:          "DC_abc",
			},
		},
		{
			name:  "edit comment with body",
			args:  "123 --edit DC_abc --body 'Updated'",
			isTTY: true,
			wantOpts: CommentOptions{
				DiscussionNumber: 123,
				EditID:           "DC_abc",
				Body:             "Updated",
			},
		},
		{
			name:  "delete comment",
			args:  "123 --delete DC_abc",
			isTTY: true,
			wantOpts: CommentOptions{
				DiscussionNumber: 123,
				DeleteID:         "DC_abc",
			},
		},
		{
			name:  "delete with yes",
			args:  "123 --delete DC_abc --yes",
			isTTY: true,
			wantOpts: CommentOptions{
				DiscussionNumber: 123,
				DeleteID:         "DC_abc",
				Yes:              true,
			},
		},
		{
			name:  "url arg overrides base repo",
			args:  "https://github.com/OTHER/REPO2/discussions/42 --body 'text'",
			isTTY: true,
			wantOpts: CommentOptions{
				DiscussionNumber: 42,
				Body:             "text",
			},
			wantBaseRepo: ghrepo.New("OTHER", "REPO2"),
		},
		{
			name:    "mutual exclusion reply-to and edit",
			args:    "123 --reply-to DC_1 --edit DC_2",
			isTTY:   true,
			wantErr: "specify only one of --reply-to, --edit, or --delete",
		},
		{
			name:    "mutual exclusion reply-to and delete",
			args:    "123 --reply-to DC_1 --delete DC_2",
			isTTY:   true,
			wantErr: "specify only one of --reply-to, --edit, or --delete",
		},
		{
			name:    "mutual exclusion body and body-file",
			args:    "123 --body 'inline' --body-file body.md",
			isTTY:   true,
			wantErr: "specify only one of --body or --body-file",
		},
		{
			name:    "delete with body is invalid",
			args:    "123 --delete DC_1 --body 'text'",
			isTTY:   true,
			wantErr: "--delete cannot be combined with --body, --body-file, or --editor",
		},
		{
			name:    "yes without delete is invalid",
			args:    "123 --yes --body 'text'",
			isTTY:   true,
			wantErr: "--yes can only be used with --delete",
		},
		{
			name:    "no body non-tty is error",
			args:    "123",
			isTTY:   false,
			wantErr: "--body or --body-file is required when not running interactively",
		},
		{
			name:    "delete without yes non-tty is error",
			args:    "123 --delete DC_1",
			isTTY:   false,
			wantErr: "--yes is required when not running interactively",
		},
		{
			name:    "no args",
			args:    "",
			isTTY:   true,
			wantErr: "accepts 1 arg(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, _, _ := iostreams.Test()
			ios.SetStdinTTY(tt.isTTY)
			ios.SetStdoutTTY(tt.isTTY)
			f := &cmdutil.Factory{IOStreams: ios}
			var gotOpts *CommentOptions
			cmd := NewCmdComment(f, func(opts *CommentOptions) error {
				gotOpts = opts
				return nil
			})
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			argv, err := shlex.Split(tt.args)
			require.NoError(t, err)
			cmd.SetArgs(argv)

			_, err = cmd.ExecuteC()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOpts.DiscussionNumber, gotOpts.DiscussionNumber)
			assert.Equal(t, tt.wantOpts.Body, gotOpts.Body)
			assert.Equal(t, tt.wantOpts.BodyFile, gotOpts.BodyFile)
			assert.Equal(t, tt.wantOpts.ReplyTo, gotOpts.ReplyTo)
			assert.Equal(t, tt.wantOpts.EditID, gotOpts.EditID)
			assert.Equal(t, tt.wantOpts.DeleteID, gotOpts.DeleteID)
			assert.Equal(t, tt.wantOpts.Yes, gotOpts.Yes)

			if tt.wantBaseRepo != nil {
				baseRepo, err := gotOpts.BaseRepo()
				require.NoError(t, err)
				assert.True(t, ghrepo.IsSame(tt.wantBaseRepo, baseRepo))
			}
		})
	}
}

func TestCommentRun(t *testing.T) {
	tests := []struct {
		name            string
		opts            CommentOptions
		bodyFileContent string
		stdinContent    string
		isTTY           bool
		setupMock       func(*client.DiscussionClientMock)
		prompter        *prompter.PrompterMock
		wantErr         string
		wantOut         string
	}{
		{
			name: "add comment with body",
			opts: CommentOptions{
				DiscussionNumber: 5,
				Body:             "Hello world",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetByNumberFunc = func(repo ghrepo.Interface, number int32) (*client.Discussion, error) {
					assert.Equal(t, int32(5), number)
					return sampleDiscussion(), nil
				}
				m.AddCommentFunc = func(repo ghrepo.Interface, discussionID, body, replyToID string) (*client.DiscussionComment, error) {
					assert.Equal(t, "D_1", discussionID)
					assert.Equal(t, "Hello world", body)
					assert.Equal(t, "", replyToID)
					return sampleComment(), nil
				}
			},
			wantOut: "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1\n",
		},
		{
			name: "add reply with body",
			opts: CommentOptions{
				DiscussionNumber: 5,
				Body:             "Reply text",
				ReplyTo:          "DC_parent",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetByNumberFunc = func(repo ghrepo.Interface, number int32) (*client.Discussion, error) {
					return sampleDiscussion(), nil
				}
				m.AddCommentFunc = func(repo ghrepo.Interface, discussionID, body, replyToID string) (*client.DiscussionComment, error) {
					assert.Equal(t, "D_1", discussionID)
					assert.Equal(t, "Reply text", body)
					assert.Equal(t, "DC_parent", replyToID)
					return sampleComment(), nil
				}
			},
			wantOut: "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1\n",
		},
		{
			name: "add comment with body-file",
			opts: CommentOptions{
				DiscussionNumber: 5,
			},
			bodyFileContent: "Body from file",
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetByNumberFunc = func(repo ghrepo.Interface, number int32) (*client.Discussion, error) {
					return sampleDiscussion(), nil
				}
				m.AddCommentFunc = func(repo ghrepo.Interface, discussionID, body, replyToID string) (*client.DiscussionComment, error) {
					assert.Equal(t, "Body from file", body)
					return sampleComment(), nil
				}
			},
			wantOut: "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1\n",
		},
		{
			name: "add comment with body-file from stdin",
			opts: CommentOptions{
				DiscussionNumber: 5,
				BodyFile:         "-",
			},
			stdinContent: "Body from stdin",
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetByNumberFunc = func(repo ghrepo.Interface, number int32) (*client.Discussion, error) {
					return sampleDiscussion(), nil
				}
				m.AddCommentFunc = func(repo ghrepo.Interface, discussionID, body, replyToID string) (*client.DiscussionComment, error) {
					assert.Equal(t, "Body from stdin", body)
					return sampleComment(), nil
				}
			},
			wantOut: "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1\n",
		},
		{
			name:  "add comment interactive editor",
			isTTY: true,
			opts: CommentOptions{
				DiscussionNumber: 5,
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetByNumberFunc = func(repo ghrepo.Interface, number int32) (*client.Discussion, error) {
					return sampleDiscussion(), nil
				}
				m.AddCommentFunc = func(repo ghrepo.Interface, discussionID, body, replyToID string) (*client.DiscussionComment, error) {
					assert.Equal(t, "Editor body", body)
					return sampleComment(), nil
				}
			},
			prompter: &prompter.PrompterMock{
				MarkdownEditorFunc: func(prompt, defaultValue string, blankAllowed bool) (string, error) {
					assert.False(t, blankAllowed)
					assert.Equal(t, "", defaultValue)
					return "Editor body", nil
				},
			},
			wantOut: "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1\n",
		},
		{
			name: "edit comment with body",
			opts: CommentOptions{
				DiscussionNumber: 5,
				EditID:           "DC_1",
				Body:             "Updated body",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					assert.Equal(t, "DC_1", commentID)
					return sampleComment(), nil
				}
				m.UpdateCommentFunc = func(repo ghrepo.Interface, commentID, body string) (*client.DiscussionComment, error) {
					assert.Equal(t, "DC_1", commentID)
					assert.Equal(t, "Updated body", body)
					return sampleComment(), nil
				}
			},
			wantOut: "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1\n",
		},
		{
			name:  "edit comment interactive editor pre-populates",
			isTTY: true,
			opts: CommentOptions{
				DiscussionNumber: 5,
				EditID:           "DC_1",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					return sampleComment(), nil
				}
				m.UpdateCommentFunc = func(repo ghrepo.Interface, commentID, body string) (*client.DiscussionComment, error) {
					assert.Equal(t, "Edited in editor", body)
					return sampleComment(), nil
				}
			},
			prompter: &prompter.PrompterMock{
				MarkdownEditorFunc: func(prompt, defaultValue string, blankAllowed bool) (string, error) {
					assert.False(t, blankAllowed)
					assert.Equal(t, "Original comment body", defaultValue)
					return "Edited in editor", nil
				},
			},
			wantOut: "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1\n",
		},
		{
			name:  "delete comment with confirmation",
			isTTY: true,
			opts: CommentOptions{
				DiscussionNumber: 5,
				DeleteID:         "DC_1",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					assert.Equal(t, "DC_1", commentID)
					return sampleComment(), nil
				}
				m.DeleteCommentFunc = func(repo ghrepo.Interface, commentID string) error {
					assert.Equal(t, "DC_1", commentID)
					return nil
				}
			},
			prompter: &prompter.PrompterMock{
				ConfirmFunc: func(prompt string, defaultValue bool) (bool, error) {
					assert.False(t, defaultValue)
					return true, nil
				},
			},
			wantOut: "",
		},
		{
			name: "delete comment with --yes skips prompt",
			opts: CommentOptions{
				DiscussionNumber: 5,
				DeleteID:         "DC_1",
				Yes:              true,
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					return sampleComment(), nil
				}
				m.DeleteCommentFunc = func(repo ghrepo.Interface, commentID string) error {
					return nil
				}
			},
			wantOut: "",
		},
		{
			name:  "delete comment declined",
			isTTY: true,
			opts: CommentOptions{
				DiscussionNumber: 5,
				DeleteID:         "DC_1",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					return sampleComment(), nil
				}
			},
			prompter: &prompter.PrompterMock{
				ConfirmFunc: func(prompt string, defaultValue bool) (bool, error) {
					return false, nil
				},
			},
			wantErr: "CancelError",
		},
		{
			name: "add comment discussion not found",
			opts: CommentOptions{
				DiscussionNumber: 5,
				Body:             "text",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetByNumberFunc = func(repo ghrepo.Interface, number int32) (*client.Discussion, error) {
					return nil, fmt.Errorf("not found")
				}
			},
			wantErr: "not found",
		},
		{
			name: "add comment mutation failed",
			opts: CommentOptions{
				DiscussionNumber: 5,
				Body:             "text",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetByNumberFunc = func(repo ghrepo.Interface, number int32) (*client.Discussion, error) {
					return sampleDiscussion(), nil
				}
				m.AddCommentFunc = func(repo ghrepo.Interface, discussionID, body, replyToID string) (*client.DiscussionComment, error) {
					return nil, fmt.Errorf("mutation failed")
				}
			},
			wantErr: "mutation failed",
		},
		{
			name: "edit comment not found",
			opts: CommentOptions{
				DiscussionNumber: 5,
				EditID:           "DC_bad",
				Body:             "text",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					return nil, fmt.Errorf("comment not found")
				}
			},
			wantErr: "comment not found",
		},
		{
			name: "edit comment mutation error",
			opts: CommentOptions{
				DiscussionNumber: 5,
				EditID:           "DC_1",
				Body:             "text",
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					return sampleComment(), nil
				}
				m.UpdateCommentFunc = func(repo ghrepo.Interface, commentID, body string) (*client.DiscussionComment, error) {
					return nil, fmt.Errorf("edit comment mutation failed")
				}
			},
			wantErr: "edit comment mutation failed",
		},
		{
			name: "delete comment not found",
			opts: CommentOptions{
				DiscussionNumber: 5,
				DeleteID:         "DC_bad",
				Yes:              true,
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					return nil, fmt.Errorf("comment not found")
				}
			},
			wantErr: "comment not found",
		},
		{
			name: "delete comment mutation error",
			opts: CommentOptions{
				DiscussionNumber: 5,
				DeleteID:         "DC_1",
				Yes:              true,
			},
			setupMock: func(m *client.DiscussionClientMock) {
				m.GetCommentFunc = func(repo ghrepo.Interface, commentID string) (*client.DiscussionComment, error) {
					return sampleComment(), nil
				}
				m.DeleteCommentFunc = func(repo ghrepo.Interface, commentID string) error {
					return fmt.Errorf("delete comment mutation failed")
				}
			},
			wantErr: "delete comment mutation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, stdin, stdout, _ := iostreams.Test()
			ios.SetStdoutTTY(tt.isTTY)
			ios.SetStdinTTY(tt.isTTY)

			if tt.stdinContent != "" {
				stdin.WriteString(tt.stdinContent)
			}

			mockClient := &client.DiscussionClientMock{}
			if tt.setupMock != nil {
				tt.setupMock(mockClient)
			}

			opts := tt.opts
			if tt.bodyFileContent != "" {
				dir := t.TempDir()
				f := filepath.Join(dir, "body.md")
				require.NoError(t, os.WriteFile(f, []byte(tt.bodyFileContent), 0600))
				opts.BodyFile = f
			}
			opts.IO = ios
			opts.BaseRepo = func() (ghrepo.Interface, error) {
				return ghrepo.New("OWNER", "REPO"), nil
			}
			opts.Client = func() (client.DiscussionClient, error) {
				return mockClient, nil
			}
			if tt.prompter != nil {
				opts.Prompter = tt.prompter
			}

			err := commentRun(&opts)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOut, stdout.String())
		})
	}
}

func sampleDiscussion() *client.Discussion {
	return &client.Discussion{
		ID:     "D_1",
		Number: 5,
		Title:  "Sample discussion",
		URL:    "https://github.com/OWNER/REPO/discussions/5",
	}
}

func sampleComment() *client.DiscussionComment {
	return &client.DiscussionComment{
		ID:   "DC_1",
		URL:  "https://github.com/OWNER/REPO/discussions/5#discussioncomment-1",
		Body: "Original comment body",
	}
}
