// Package email parses the deliberately bounded RFC822 input of the trusted demo
// importer. It does not authenticate a real SMTP sender or deliver mail.
package email

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxRaw = 4 << 20
const MaxAttachment = 1 << 20
const MaxAttachmentTotal = 2 << 20

var ErrInvalid = errors.New("invalid or unsupported email")

type Attachment struct {
	Filename  string
	MediaType string
	Data      []byte
}
type Message struct {
	ID, Sender, Subject, Body string
	TicketID                  int64
	Attachments               []Attachment
}

var reference = regexp.MustCompile(`#([0-9]+)\b`)

func Parse(raw []byte) (Message, error) {
	var result Message
	if len(raw) == 0 || len(raw) > MaxRaw {
		return result, ErrInvalid
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return result, ErrInvalid
	}
	for _, key := range []string{"Message-Id", "From", "Subject"} {
		if len(msg.Header[textproto.CanonicalMIMEHeaderKey(key)]) != 1 {
			return result, ErrInvalid
		}
	}
	result.ID = strings.TrimSpace(msg.Header.Get("Message-ID"))
	if len(result.ID) < 5 || len(result.ID) > 255 || result.ID[0] != '<' || result.ID[len(result.ID)-1] != '>' || strings.Count(result.ID, "@") != 1 {
		return result, ErrInvalid
	}
	for _, c := range result.ID[1 : len(result.ID)-1] {
		if c <= 32 || c >= 127 || c == '<' || c == '>' {
			return result, ErrInvalid
		}
	}
	senders, err := mail.ParseAddressList(msg.Header.Get("From"))
	if err != nil || len(senders) != 1 {
		return result, ErrInvalid
	}
	result.Sender = strings.ToLower(senders[0].Address)
	result.Subject, err = new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || !validText(result.Subject) {
		return result, ErrInvalid
	}
	result.Subject = strings.TrimSpace(result.Subject)
	if utf8.RuneCountInString(result.Subject) < 1 || utf8.RuneCountInString(result.Subject) > 200 {
		return result, ErrInvalid
	}
	for _, match := range reference.FindAllStringSubmatch(result.Subject, -1) {
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || id < 1 || (result.TicketID != 0 && result.TicketID != id) {
			return result, ErrInvalid
		}
		result.TicketID = id
	}
	parts, total := 0, 0
	body, err := readPart(textproto.MIMEHeader(msg.Header), msg.Body, 0, &parts, &total, &result.Attachments)
	if err != nil {
		return result, err
	}
	result.Body = CleanBody(body)
	if result.Body == "" && len(result.Attachments) > 0 {
		result.Body = "E-posta ekleri iletildi."
	}
	if !validText(result.Body) || utf8.RuneCountInString(result.Body) < 1 || utf8.RuneCountInString(result.Body) > 10000 {
		return result, ErrInvalid
	}
	return result, nil
}
func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
func readPart(h textproto.MIMEHeader, r io.Reader, depth int, parts, total *int, attachments *[]Attachment) (string, error) {
	*parts++
	if depth > 8 || *parts > 40 {
		return "", ErrInvalid
	}
	contentType := h.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", ErrInvalid
	}
	disposition := ""
	var disp map[string]string
	if value := h.Get("Content-Disposition"); value != "" {
		disposition, disp, err = mime.ParseMediaType(value)
		if err != nil {
			return "", ErrInvalid
		}
	}
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
	case "", "7bit", "8bit", "binary":
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	default:
		return "", ErrInvalid
	}
	filename := disp["filename"]
	if filename == "" {
		filename = params["name"]
	}
	if disposition == "attachment" || filename != "" {
		if len(*attachments) >= 10 {
			return "", ErrInvalid
		}
		data, err := io.ReadAll(io.LimitReader(r, MaxAttachment+1))
		if err != nil || len(data) > MaxAttachment {
			return "", ErrInvalid
		}
		*total += len(data)
		if *total > MaxAttachmentTotal {
			return "", ErrInvalid
		}
		filename, err = new(mime.WordDecoder).DecodeHeader(filename)
		if err != nil {
			return "", ErrInvalid
		}
		filename = SafeFilename(filename)
		*attachments = append(*attachments, Attachment{Filename: filename, MediaType: media, Data: data})
		return "", nil
	}
	if strings.HasPrefix(media, "multipart/") {
		if params["boundary"] == "" {
			return "", ErrInvalid
		}
		reader := multipart.NewReader(r, params["boundary"])
		var texts []string
		for {
			p, err := reader.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", ErrInvalid
			}
			text, err := readPart(p.Header, p, depth+1, parts, total, attachments)
			closeErr := p.Close()
			if err != nil {
				return "", err
			}
			if closeErr != nil {
				return "", ErrInvalid
			}
			if text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n"), nil
	}
	if media != "text/plain" {
		return "", nil
	} // HTML is never converted or rendered.
	charset := strings.ToLower(params["charset"])
	if charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return "", ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(r, 64*1024+1))
	if err != nil || len(data) > 64*1024 || !validText(string(data)) {
		return "", ErrInvalid
	}
	return string(data), nil
}
func SafeFilename(s string) string {
	s = path.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > 120 {
		s = string(runes[:120])
	}
	if s == "" || s == "." || s == ".." || !utf8.ValidString(s) {
		return "ek.bin"
	}
	return s
}

// Delimiters are intentionally conservative: arbitrary words in a customer's
// explanation are not treated as signatures. Quoted lines never become a reply.
func CleanBody(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if line == "-- " || trimmed == "--" || lower == "saygılarımla," || lower == "sent from my iphone" || lower == "-----original message-----" || lower == "-----özgün ileti-----" || (strings.HasPrefix(lower, "on ") && strings.HasSuffix(lower, " wrote:")) || strings.HasSuffix(lower, " yazdı:") {
			break
		}
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
