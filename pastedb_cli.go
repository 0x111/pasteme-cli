package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/pbkdf2"
)

type Paste struct {
	Paste struct {
		Name struct {
			Data string `json:"data"`
			Iv   string `json:"iv"`
			Salt string `json:"salt"`
		} `json:"name"`
		Body struct {
			Data string `json:"data"`
			Iv   string `json:"iv"`
			Salt string `json:"salt"`
		} `json:"body"`
	} `json:"paste"`
	Files          []Files `bson:"files" json:"files"`
	SourceCode     bool    `json:"sourceCode"`
	SelfDestruct   bool    `json:"selfDestruct"`
	ExpiresMinutes int64   `json:"expiresMinutes"`
}

type Files struct {
	Name struct {
		Data   string `bson:"data" json:"data"`
		Vector string `bson:"iv" json:"iv"`
		Salt   string `bson:"salt" json:"salt"`
	} `json:"name"`
	Content struct {
		Data   string `bson:"data" json:"data"`
		Vector string `bson:"iv" json:"iv"`
		Salt   string `bson:"salt" json:"salt"`
	} `json:"content"`
}

type PasteSuccess struct {
	Msg   string `json:"msg"`
	Paste struct {
		Uuid string `json:"uuid"`
	} `json:"paste"`
}

func main() {
	app := cli.NewApp()
	app.Name = "Paste.me"
	app.Version = "v0.0.2"
	app.Usage = "Share your pastes securely"

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "name",
			Usage: "Insert the name of the paste here.",
		},
		&cli.StringFlag{
			Name:  "body",
			Usage: "Here you can insert the paste body or send it through cli.",
		},
		&cli.Int64Flag{
			Name:  "expire",
			Usage: "Here you will be able to set an expiration time for your pastes. The expiration time should be defined in minutes. Allowed values for the time being: 5,10,60,1440,10080,43800.",
		},
		&cli.BoolFlag{
			Name:  "destroy",
			Usage: "With this flag, you are posting the paste with a 'Self Destruct' flag. The link will work only once.",
		},
		&cli.BoolFlag{
			Name:  "source",
			Usage: "With this flag, you are posting a paste which is some kind of source code. Syntax highlighting will be applied.",
		},
	}

	app.Action = Action

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func Action(c *cli.Context) error {
	var err error
	name := c.String("name")
	sourceCode := c.Bool("source")
	destroy := c.Bool("destroy")
	body := c.String("body")
	minutes := c.Int64("expire")
	pasteText := ""

	if len(name) == 0 {
		fmt.Println("Please provide a name for your paste. Use the --help if in doubt.")
		return errors.New("paste_name_error")
	}

	var terminalText string
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		terminalText, err = ReadDataFromTerminal(os.Stdin)
		if err != nil {
			return err
		}
	} else {
		if len(body) > 0 {
			terminalText, err = ReadDataFromTerminal(strings.NewReader(body))
		} else {
			terminalText = ""
		}
	}

	if len(terminalText) > 0 {
		pasteText = terminalText
	} else {
		pasteText = body
	}

	if err != nil || len(pasteText) == 0 {
		fmt.Println("Your paste has a length of 0. Try again, but this time try to put some content.")
		return errors.New("paste_length_error")
	}

	if !destroy && !IsValidMinutes(minutes) {
		fmt.Println("You did not provide a valid minutes flag. See --help for more insight on this one.")
		return errors.New("expire_not_found")
	}

	rb, _ := GenerateRandomBytes(28)

	//rand.Read(rb)
	h := sha256.New()
	h.Write(rb)
	passPhrase := hex.EncodeToString(h.Sum(nil))
	encryptName := encrypt(passPhrase, name)
	encryptData := encrypt(passPhrase, pasteText)
	splittedEncryptName := strings.Split(encryptName, "-")
	splittedEncryptData := strings.Split(encryptData, "-")
	//split encrypt result
	paste := &Paste{}
	paste.Paste.Name.Data = splittedEncryptName[2]
	paste.Paste.Name.Iv = splittedEncryptName[1]
	paste.Paste.Name.Salt = splittedEncryptName[0]
	paste.Paste.Body.Data = splittedEncryptData[2]
	paste.Paste.Body.Iv = splittedEncryptData[1]
	paste.Paste.Body.Salt = splittedEncryptData[0]
	paste.SourceCode = sourceCode
	paste.SelfDestruct = destroy
	// Note: The cli client does not support sending files yet!
	paste.Files = []Files{}

	if !destroy {
		paste.ExpiresMinutes = minutes
	}

	jsonValue, _ := json.Marshal(paste)

	req, err := http.NewRequest("POST", "https://api.paste.me/api/paste/new", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		log.Fatal(err)
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		return cli.NewExitError("There was some problem while sending the paste data. Please try again later or contact the site administrator.", 15)
	}

	if resp.StatusCode == 200 {
		res := PasteSuccess{}
		err = json.Unmarshal(bodyBytes, &res)

		if err != nil {
			return cli.NewExitError("We received an invalid response from the server. Please contact the site administrator.", 16)
		}

		msg := `Paste added successfully!
Share this url to your friends: https://paste.me/paste/` + res.Paste.Uuid + `#` + passPhrase
		fmt.Println(msg)
		return nil
	} else {
		return cli.NewExitError("There was some error while pasting your data. Please try again later or contact the Paste.me admin!", 17)
	}

	return nil
}

// @SRC: https://gist.github.com/dopey/c69559607800d2f2f90b1b1ed4e550fb
// GenerateRandomBytes returns securely generated random bytes.
// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}

func ReadDataFromTerminal(r io.Reader) (string, error) {
	var result string
	rBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}
	result = string(rBytes)
	return result, nil
}

// @SRC: https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28
func deriveKey(passphrase string, salt []byte) ([]byte, []byte) {
	if salt == nil {
		salt = make([]byte, 8)
		// http://www.ietf.org/rfc/rfc2898.txt
		// Salt.
		rand.Read(salt)
	}
	return pbkdf2.Key([]byte(passphrase), salt, 1000, 32, sha256.New), salt
}

// @SRC: https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28
func encrypt(passphrase, plaintext string) string {
	key, salt := deriveKey(passphrase, nil)
	iv := make([]byte, 12)
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	rand.Read(iv)
	b, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(b)
	data := aesgcm.Seal(nil, iv, []byte(plaintext), nil)
	return hex.EncodeToString(salt) + "-" + hex.EncodeToString(iv) + "-" + hex.EncodeToString(data)
}

func IsValidMinutes(minutes int64) bool {
	switch minutes {
	case
		5, 10, 60, 1440, 10080, 43800:
		return true
	}
	return false
}
