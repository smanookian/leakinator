// Command mkfixtures makes the test videos in testdata/videos.
//
// Fake keys are built at run time from a fixed seed, so no key-like string
// sits in the source (GitHub push protection would block it).
//
//	go run ./tools/mkfixtures            # small test videos (committed)
//	go run ./tools/mkfixtures -long out.mp4   # 20-minute 1080p speed test video
package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	width  = 1920
	height = 1080
	bg     = "0x1e1e1e"
	fg     = "0xd4d4d4"
)

var font = flag.String("font", "/usr/share/fonts/TTF/DejaVuSansMono.ttf", "monospace TTF font")

func main() {
	out := flag.String("out", "testdata/videos", "output folder")
	long := flag.String("long", "", "write the 20-minute speed test video to this file and exit")
	flag.Parse()

	tmp, err := os.MkdirTemp("", "mkfixtures")
	check(err)
	defer os.RemoveAll(tmp)

	if *long != "" {
		makeLong(tmp, *long)
		return
	}
	check(os.MkdirAll(*out, 0o755))
	k := fakeKeys()
	makeEnvKey(tmp, filepath.Join(*out, "env_key.mp4"))
	makePatternKeys(tmp, filepath.Join(*out, "pattern_keys.mp4"), k)
	makeClean(tmp, filepath.Join(*out, "clean.mp4"))
	makeSmallText(tmp, filepath.Join(*out, "small_text.mp4"))
	makeLoudAudio(tmp, filepath.Join(*out, "loud_audio.mp4"))
}

// Values from testdata/test.env. They match no known key pattern on purpose,
// so only the exact (.env) match can find them.
const (
	envToken    = "tk_9fQ2xZ7pLm4Rv9sK3wYb8Hc"
	envPassword = "Hunter2-Correct-Horse-91"
)

type keys struct {
	openai, anthropic, github, awsID, awsSecret, stripe, slack, jwt string
	privBody                                                       []string
}

func fakeKeys() keys {
	r := rand.New(rand.NewPCG(42, 0))
	const b62 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	s := func(n int, set string) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = set[r.IntN(len(set))]
		}
		return string(b)
	}
	digits := "0123456789"
	b64 := b62 + "+/"
	var k keys
	k.openai = "sk-" + "proj-" + s(40, b62)
	k.anthropic = "sk-" + "ant-api03-" + s(40, b62)
	k.github = "gh" + "p_" + s(36, b62)
	k.awsID = "AK" + "IA" + s(16, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")
	k.awsSecret = s(40, b64)
	k.stripe = "sk_" + "live_" + s(24, b62)
	k.slack = "xo" + "xb-" + s(12, digits) + "-" + s(13, digits) + "-" + s(24, b62)
	enc := base64.RawURLEncoding
	k.jwt = enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." +
		enc.EncodeToString([]byte(`{"sub":"42"}`)) + "." + s(43, b62)
	for range 3 {
		k.privBody = append(k.privBody, s(64, b64))
	}
	return k
}

// scene is text shown from start to end (seconds) at a font size.
type scene struct {
	start, end float64
	size       int
	x, y       int
	lines      []string
}

const editorCode = `package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	addr := os.Getenv("ADDR")
	http.HandleFunc("/", hello)
	fmt.Println("listening on", addr)
	http.ListenAndServe(addr, nil)
}`

func codeLines() []string { return strings.Split(strings.ReplaceAll(editorCode, "\t", "    "), "\n") }

// render draws scenes on a dark background and encodes the video.
// audio is an ffmpeg lavfi audio source+filter, or "" for no audio.
func render(tmp, out string, dur float64, fps int, scenes []scene, audio string) {
	var filters []string
	for i, sc := range scenes {
		tf := filepath.Join(tmp, fmt.Sprintf("%s-%d.txt", filepath.Base(out), i))
		check(os.WriteFile(tf, []byte(strings.Join(sc.lines, "\n")), 0o644))
		filters = append(filters, fmt.Sprintf(
			"drawtext=fontfile=%s:textfile=%s:expansion=none:fontsize=%d:fontcolor=%s:x=%d:y=%d:line_spacing=%d:enable='between(t,%g,%g)'",
			*font, tf, sc.size, fg, sc.x, sc.y, sc.size/2, sc.start, sc.end-0.001))
	}
	vf := "null"
	if len(filters) > 0 {
		vf = strings.Join(filters, ",")
	}
	args := []string{"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=%dx%d:r=%d:d=%g", bg, width, height, fps, dur)}
	if audio != "" {
		args = append(args, "-f", "lavfi", "-i", audio)
	}
	args = append(args, "-vf", vf, "-c:v", "libx264", "-preset", "medium", "-crf", "23", "-pix_fmt", "yuv420p",
		"-x264-params", "keyint=" + fmt.Sprint(fps*10))
	if audio != "" {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-shortest")
	}
	args = append(args, "-t", fmt.Sprint(dur), "-movflags", "+faststart", out)
	run(args...)
	fmt.Println("wrote", out)
}

func makeEnvKey(tmp, out string) {
	code := codeLines()
	render(tmp, out, 12, 10, []scene{
		{0, 12, 28, 80, 60, code},
		// .env value shown in a terminal for 2 seconds: 5.0-7.0.
		{5, 7, 28, 80, 700, []string{"$ cat .env", "MY_API_TOKEN=" + envToken}},
		// Password cut off at the right screen edge: 9.0-11.0.
		{9, 11, 28, 1450, 800, []string{"db password: " + envPassword}},
	}, "")
}

func makePatternKeys(tmp, out string, k keys) {
	code := codeLines()[:7]
	shown := [][]string{
		{"OPENAI_API_KEY=" + k.openai},
		{"ANTHROPIC_API_KEY=" + k.anthropic},
		{"git remote set-url origin https://" + k.github + "@github.com/me/app"},
		{"aws_access_key_id = " + k.awsID, "aws_secret_access_key = " + k.awsSecret},
		{"stripe.api_key = \"" + k.stripe + "\""},
		{"SLACK_BOT_TOKEN=" + k.slack},
		{"Authorization: Bearer " + k.jwt},
		append(append([]string{"-----BEGIN RSA PRIVATE KEY-----"}, k.privBody...), "-----END RSA PRIVATE KEY-----"),
	}
	scenes := []scene{{0, 18, 28, 80, 60, code}}
	for i, lines := range shown {
		t := float64(1 + 2*i)
		scenes = append(scenes, scene{t, t + 2, 28, 80, 600, lines})
	}
	render(tmp, out, 18, 10, scenes, "")
}

func makeClean(tmp, out string) {
	lines := []string{
		"// Keys come from the environment, never from code.",
		`const openai = new OpenAI({ apiKey: process.env.OPENAI_API_KEY });`,
		`const stripe = require("stripe")(process.env.STRIPE_SECRET_KEY);`,
		`// GitHub tokens start with ghp_ and Stripe keys with sk_live_`,
		`const id = "3f2c9a1e-7b4d-4e8a-9c1f-2d5e6b7a8c90"; // request id`,
		`// commit 9fceb02d0ae598e95dc970b74767f19372d61af8`,
		`const sha = createHash("sha256").update(body).digest("hex");`,
		`const port = Number(process.env.PORT ?? 3000);`,
		`app.listen(port, () => console.log("ready on", port));`,
	}
	render(tmp, out, 15, 10, []scene{
		{0, 15, 28, 80, 60, codeLines()},
		{0, 15, 28, 80, 640, lines},
	}, "anoisesrc=color=pink:amplitude=0.25:seed=7,lowpass=f=4000,volume=-1.5dB,atrim=0:15")
}

func makeSmallText(tmp, out string) {
	code := codeLines()
	var small []string
	for range 4 {
		small = append(small, code...)
	}
	render(tmp, out, 12, 10, []scene{
		{0, 4, 28, 80, 60, code},
		{4, 9, 13, 40, 30, small},
		{9, 12, 28, 80, 60, code},
	}, "")
}

func makeLoudAudio(tmp, out string) {
	// 0-6 s loud noise, hard clipped at 2-4 s; 6-12 s silence; 12-20 s noise.
	a := "anoisesrc=color=pink:amplitude=0.5:seed=3,lowpass=f=5000," +
		"volume=enable='between(t,2,4)':volume=18dB," +
		"volume=enable='between(t,6,12)':volume=0," +
		"aformat=sample_fmts=s16,atrim=0:20"
	render(tmp, out, 20, 10, []scene{{0, 20, 28, 80, 60, codeLines()}}, a)
}

// makeLong writes a 20-minute 1080p30 screen-recording-like video:
// a new code page every 15 s, a moving mouse pointer, a blinking cursor,
// and the .env token on screen at 10:00-10:03.
func makeLong(tmp, out string) {
	r := rand.New(rand.NewPCG(1, 0))
	title := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
	words := []string{"user", "order", "cache", "item", "price", "total", "request", "client", "config", "result"}
	page := func() []string {
		var lines []string
		for len(lines) < 26 {
			w := words[r.IntN(len(words))]
			v := words[r.IntN(len(words))]
			switch r.IntN(4) {
			case 0:
				lines = append(lines, fmt.Sprintf("func load%s(%s *%s) error {", title(w), v, title(v)))
			case 1:
				lines = append(lines, fmt.Sprintf("    %s, err := db.Get%s(ctx, %s.ID)", w, title(v), v))
			case 2:
				lines = append(lines, "    if err != nil {", fmt.Sprintf("        return fmt.Errorf(\"load %s: %%w\", err)", w), "    }")
			default:
				lines = append(lines, fmt.Sprintf("    %s.Total += %s.Price * %d", w, v, r.IntN(100)))
			}
		}
		return lines
	}
	const dur = 1200.0
	var filters []string
	n := 0
	add := func(start, end float64, size, x, y int, lines []string) {
		tf := filepath.Join(tmp, fmt.Sprintf("p%d.txt", n))
		n++
		check(os.WriteFile(tf, []byte(strings.Join(lines, "\n")), 0o644))
		filters = append(filters, fmt.Sprintf(
			"drawtext=fontfile=%s:textfile=%s:expansion=none:fontsize=%d:fontcolor=%s:x=%d:y=%d:line_spacing=%d:enable='between(t,%g,%g)'",
			*font, tf, size, fg, x, y, size/2, start, end-0.001))
	}
	for t := 0.0; t < dur; t += 15 {
		add(t, t+15, 26, 80, 40, page())
	}
	add(600, 603, 26, 80, 1000, []string{"MY_API_TOKEN=" + envToken})
	filters = append(filters,
		// blinking text cursor
		"drawbox=x=1200:y=500:w=3:h=28:color=white:t=fill:enable='lt(mod(t,1),0.5)'",
		// mouse pointer wandering around
		"drawbox=x='900+700*sin(t/7)':y='500+400*sin(t/5)':w=14:h=20:color=white:t=fill")
	run("-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=%dx%d:r=30:d=%g", bg, width, height, dur),
		"-f", "lavfi", "-i", "anoisesrc=color=pink:amplitude=0.25:seed=9,lowpass=f=4000",
		"-vf", strings.Join(filters, ","),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-t", fmt.Sprint(dur), "-movflags", "+faststart", out)
	fmt.Println("wrote", out)
}

func run(args ...string) {
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	check(cmd.Run())
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkfixtures:", err)
		os.Exit(1)
	}
}
