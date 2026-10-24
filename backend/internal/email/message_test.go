package email

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func raw(subject, body string) []byte {
	return []byte("Message-ID: <demo@example.local>\r\nFrom: Çalışan <calisan@destekmasa.local>\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body)
}
func TestParsePlainAndClean(t *testing.T) {
	m, err := Parse(raw("=?UTF-8?B?QmHEn2xhbnTEsQ==?=", "Yeni bilgi\r\n\r\nOn Monday, Demo wrote:\r\n> Eski mesaj\r\n-- \r\nİmza"))
	if err != nil || m.Subject != "Bağlantı" || m.Body != "Yeni bilgi" || m.Sender != "calisan@destekmasa.local" {
		t.Fatalf("%+v %v", m, err)
	}
	for _, test := range []struct{ input, want string }{
		{"Açıklama\n-- \nİmza", "Açıklama"}, {"Açıklama\nSaygılarımla,\nAd", "Açıklama"}, {"Yanıt\n24 Ekim tarihinde Ali yazdı:\nAlıntı", "Yanıt"}, {"Yeni\n> quoted\nDevam", "Yeni\nDevam"}, {"Açıklamada imza sorunu var", "Açıklamada imza sorunu var"}, {"Yeni\n-----Original Message-----\nOld", "Yeni"},
	} {
		if got := CleanBody(test.input); got != test.want {
			t.Fatalf("%q != %q", got, test.want)
		}
	}
}
func TestMultipartAndAttachment(t *testing.T) {
	data := "Message-ID: <parts@example.local>\r\nFrom: calisan@destekmasa.local\r\nSubject: Re: #123\r\nContent-Type: multipart/mixed; boundary=outer\r\n\r\n--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n--inner\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nYeni =C5=9Fikayet\r\n--inner\r\nContent-Type: text/html\r\n\r\n<script>bad()</script>\r\n--inner--\r\n--outer\r\nContent-Type: text/plain; name=log.txt\r\nContent-Disposition: attachment; filename=\"../../log.txt\"\r\nContent-Transfer-Encoding: base64\r\n\r\nAAECAw==\r\n--outer--\r\n"
	m, err := Parse([]byte(data))
	if err != nil || m.TicketID != 123 || m.Body != "Yeni şikayet" || len(m.Attachments) != 1 {
		t.Fatalf("%+v %v", m, err)
	}
	a := m.Attachments[0]
	if a.Filename != "log.txt" || string(a.Data) != string([]byte{0, 1, 2, 3}) {
		t.Fatalf("%+v", a)
	}
}
func TestInvalidMessages(t *testing.T) {
	valid := string(raw("Konu", "Metin"))
	for name, input := range map[string]string{
		"missing id":      strings.Replace(valid, "Message-ID: <demo@example.local>\r\n", "", 1),
		"duplicate id":    strings.Replace(valid, "Message-ID:", "Message-ID: <other@example.local>\r\nMessage-ID:", 1),
		"invalid id":      strings.Replace(valid, "<demo@example.local>", "<bad id@example.local>", 1),
		"two from":        strings.Replace(valid, "calisan@destekmasa.local>", "calisan@destekmasa.local>, x@example.local", 1),
		"unknown charset": strings.Replace(valid, "charset=utf-8", "charset=unknown", 1),
		"html only":       strings.Replace(valid, "text/plain", "text/html", 1),
		"empty":           string(raw("Konu", "> only quote")),
		"nul":             string(raw("Konu", "text\x00")),
		"large body":      string(raw("Konu", strings.Repeat("a", 10001))),
		"multiple refs":   string(raw("Re: #1 and #2", "Reply")),
		"zero ref":        string(raw("Re: #0", "Reply")),
		"overflow ref":    string(raw("Re: #9999999999999999999999999", "Reply")),
		"bad base64":      strings.Replace(valid, "Content-Type:", "Content-Transfer-Encoding: base64\r\nContent-Type:", 1),
		"raw too large":   strings.Repeat("a", MaxRaw+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatal("invalid email accepted")
			}
		})
	}
}
func attachmentMail(sizes ...int) []byte {
	var b strings.Builder
	b.WriteString("Message-ID: <limits@example.local>\r\nFrom: calisan@destekmasa.local\r\nSubject: Ek\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n")
	for i, size := range sizes {
		fmt.Fprintf(&b, "--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"%d.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", i, base64.StdEncoding.EncodeToString(make([]byte, size)))
	}
	b.WriteString("--b--\r\n")
	return []byte(b.String())
}
func TestAttachmentLimits(t *testing.T) {
	m, err := Parse(attachmentMail(MaxAttachment, MaxAttachment))
	if err != nil || len(m.Attachments) != 2 || m.Body != "E-posta ekleri iletildi." {
		t.Fatalf("limits: %+v %v", m, err)
	}
	for _, sizes := range [][]int{{MaxAttachment + 1}, {MaxAttachment, MaxAttachment, 1}, {1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}} {
		if _, err := Parse(attachmentMail(sizes...)); err == nil {
			t.Fatal("limit accepted")
		}
	}
}
func TestMIMEDepthLimit(t *testing.T) {
	content := "Content-Type: text/plain\r\n\r\ntext\r\n"
	for i := 0; i < 10; i++ {
		boundary := fmt.Sprintf("nested%d", i)
		content = "Content-Type: multipart/mixed; boundary=" + boundary + "\r\n\r\n--" + boundary + "\r\n" + content + "\r\n--" + boundary + "--\r\n"
	}
	data := []byte("Message-ID: <deep@example.local>\r\nFrom: calisan@destekmasa.local\r\nSubject: Deep\r\n" + content)
	if _, err := Parse(data); err == nil {
		t.Fatal("deep mime accepted")
	}
}
