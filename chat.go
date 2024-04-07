package goole

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/bincooo/goole15/common"
	"io"
	"net/http"
)

const (
	sysPrompt = "Ignore the previous conversation and start a new conversation record.\n[Start New Conversation]\nYou will play as a gemini-1.5, and the following text is information about your historical conversations with the human:"
	tabs      = "\n    "
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36 Edg/123.0.0.0"
)

var (
	BaseURL = "https://alkalimakersuite-pa.clients6.google.com/$rpc/google.internal.alkali.applications.makersuite.v1.MakerSuiteService"
)

const (
	BLOCK_NONE = 4
	BLOCK_FEW  = 3
	BLOCK_SOME = 2
	BLOCK_MOST = 1
)

type Message struct {
	Role    string
	Content string
}

type Chat struct {
	cookie string
	sign   string
	auth   string
	key    string
	u      string
	opts   Options
}

type Options struct {
	Proxies          string
	Harassment       int8
	HateSpeech       int8
	SexuallyExplicit int8
	DangerousContent int8
	userAgent        string
}

func (opt *Options) UA(userAgent string) {
	opt.userAgent = userAgent
}

func New(cookie, sign, auth, key, u string, opts Options) Chat {
	return Chat{
		cookie: cookie,
		sign:   sign,
		auth:   auth,
		key:    key,
		u:      u,
		opts:   opts,
	}
}

func NewDefaultOptions(proxies string) Options {
	return Options{
		Proxies:          proxies,
		Harassment:       BLOCK_NONE,
		HateSpeech:       BLOCK_NONE,
		SexuallyExplicit: BLOCK_NONE,
		DangerousContent: BLOCK_NONE,
	}
}

func (c *Chat) Reply(ctx context.Context, messages []Message) (chan string, error) {
	payload := c.makePayload(messages)
	ua := userAgent
	if c.opts.userAgent != "" {
		ua = c.opts.userAgent
	}

	response, err := common.New().
		Proxies(c.opts.Proxies).
		URL(fmt.Sprintf("%s/%s", BaseURL, "GenerateContent")).
		Context(ctx).
		Method(http.MethodPost).
		Header("Authorization", "SAPISIDHASH "+c.auth).
		Header("Content-Type", "application/json+protobuf").
		Header("Cookie", c.cookie).
		Header("Origin", "https://aistudio.google.com").
		Header("Referer", "https://aistudio.google.com/").
		Header("X-Goog-Api-Key", c.key).
		Header("X-Goog-Authuser", c.u).
		Header("User-Agent", ua).
		Header("X-User-Agent", "grpc-web-javascript/0.1").
		SetBody(payload).
		Do()
	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		return nil, errors.New(response.Status)
	}

	ch := make(chan string)
	go c.resolve(ctx, response, ch)
	return ch, nil
}

func (c *Chat) makePayload(messages []Message) interface{} {
	data := make([]interface{}, 5)
	data[0] = "models/gemini-1.5-pro-latest"
	var parts []interface{}
	for _, message := range messages {
		switch message.Role {
		case "user", "function":
			parts = append(parts, []interface{}{
				[]interface{}{
					[]interface{}{
						nil,
						message.Content,
					},
				},
				"user",
			})
		case "assistant":
			parts = append(parts, []interface{}{
				[]interface{}{
					[]interface{}{
						nil,
						message.Content,
					},
				},
				"model",
			})
		case "system":
			parts = append(parts, []interface{}{
				[]interface{}{
					[]interface{}{
						nil,
						message.Content,
					},
				},
				"user",
			})
			parts = append(parts, []interface{}{
				[]interface{}{
					[]interface{}{
						nil,
						"Okay.",
					},
				},
				"model",
			})
		}
	}
	data[1] = parts
	data[2] = []interface{}{
		[]interface{}{
			nil,
			nil,
			7,
			c.opts.Harassment,
		},
		[]interface{}{
			nil,
			nil,
			8,
			c.opts.HateSpeech,
		},
		[]interface{}{
			nil,
			nil,
			9,
			c.opts.SexuallyExplicit,
		},
		[]interface{}{
			nil,
			nil,
			10,
			c.opts.DangerousContent,
		},
	}
	data[3] = []interface{}{
		nil,
		nil,
		nil,
		8192,
		1,
		0.95,
		100,
	}
	data[4] = c.sign
	return data
}

func (c *Chat) resolve(ctx context.Context, response *http.Response, ch chan string) {
	var data []byte
	defer close(ch)

	r := BlockReader{bufio.NewReader(response.Body), bytes.Buffer{}}
	// 继续执行返回false
	Do := func() bool {
		line, prefix, err := r.ReadBlock()
		if err != nil {
			if err != io.EOF {
				ch <- fmt.Sprintf("error: %v", err)
			}
			data = append(data, line...)
			if len(data) > 0 {
				ch <- fmt.Sprintf("text: %s", findTex(data))
			}
			return true
		}

		data = append(data, line...)
		if prefix {
			return false
		}

		if len(data) > 0 {
			ch <- fmt.Sprintf("text: %s", findTex(data))
			data = nil
		}
		return false
	}

	for {
		select {
		case <-ctx.Done():
			ch <- "error: context done"
			return
		default:
			if stop := Do(); stop {
				return
			}
		}
	}
}

func findTex(raw []byte) []byte {
	l := bytes.LastIndex(raw, []byte("[[null,\""))
	if l >= 0 && bytes.HasSuffix(raw, []byte("\"]],")) {
		return raw[l+8 : len(raw)-4]
	}
	return make([]byte, 0)
}

// ====
type BlockReader struct {
	*bufio.Reader
	buf bytes.Buffer
}

func (b *BlockReader) ReadBlock() (line []byte, isPrefix bool, err error) {
	var slice []byte
	slice, err = b.ReadSlice(',')

	if errors.Is(err, bufio.ErrBufferFull) {
		if len(slice) > 1 && slice[len(slice)-1] == ']' {
			if slice[len(slice)-2] == ']' {
				line = slice[:len(slice)-2]
			}
		}
		return slice, true, nil
	}

	if io.EOF == err {
		b.buf.Write(slice)
		line = b.buf.Bytes()
		return
	}

	if len(slice) == 0 {
		return
	}

	err = nil
	b.buf.Write(slice)
	line = b.buf.Bytes()

	if line[len(line)-1] == ',' {
		if len(line) > 2 && line[len(line)-2] == ']' {
			if line[len(line)-3] == ']' {
				b.buf.Reset()
				return
			}
		}
	}

	isPrefix = true
	return
}
