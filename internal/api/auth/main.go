package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"terrapak/internal/api/auth/jwt"
	"terrapak/internal/api/auth/providers/github"
	"terrapak/internal/api/auth/roles"
	"terrapak/internal/api/auth/types"
	"terrapak/internal/config"
	"terrapak/internal/db/entity"
	"terrapak/internal/db/services"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"gopkg.in/boj/redistore.v1"
)

var (
	// codeVerifier = generateCodeVerifier()
	//codeChallenge = generateCodeChallenge(codeVerifier)
)

type AuthProvider interface {
	Name() string
	Config() (conf oauth2.Config)
	Callback(token string)
	UserEmail(token string) (string,error)
	UserInfo(token string) (types.UserInfo,error)
}

type TokenRequest struct {
	Code string `json:"code" form:"code"`
	ClientID string `json:"client_id" form:"client_id"`
	CodeVerifier string `json:"code_verifier" form:"code_verifier"`
	GrantType string `json:"grant_type" form:"grant_type"`
}

type OAuthToken struct {
	AccessToken string `json:"access_token"`
}

func GetAuthProvider() AuthProvider {
	gc := config.GetDefault()
	switch gc.AuthProvider.Type {
		case "github":
			github := github.New()
			return github
	}
	return nil

}


func Authorize(c *gin.Context) {
	//..
	store, err := redistore.NewRediStore(10, "tcp", ":6379", "", []byte(os.Getenv("TP_SECRET"))); if err != nil {
		c.JSON(500, gin.H{
			"redis_error": err.Error(),
		})
		return
	}
	gc := config.GetDefault()
	
	sessions, _ := store.Get(c.Request, "mysession")
	sessions.Options.MaxAge = 60 * 2
	state := c.Query("state")
	sessions.Values["state"] = state

	redirect := c.Query("redirect_uri")
	sessions.Values["redirect_uri"] = redirect

	provider := GetAuthProvider()
	sessions.Save(c.Request, c.Writer)

	conf := provider.Config()
	conf.RedirectURL = fmt.Sprintf("https://%s/v1/auth/callback", gc.Hostname)
	url := conf.AuthCodeURL(state,oauth2.SetAuthURLParam("code_challenge", c.Query("code_challenge")),oauth2.SetAuthURLParam("code_challenge_method", "S256"))
	c.Redirect(302, url)
}

func Token(c *gin.Context) {
	tokenRequest := TokenRequest{}
	err := c.Bind(&tokenRequest); if err != nil {
		if e, ok := err.(*json.SyntaxError); ok {
			fmt.Printf("syntax error at byte offset %d", e.Offset)
		}
		c.JSON(400, gin.H{
			"error": err.Error(),
		})
		return
	}

	provider := GetAuthProvider()
	conf := provider.Config()
	token, err := conf.Exchange(c, tokenRequest.Code, oauth2.SetAuthURLParam("code_verifier", tokenRequest.CodeVerifier)); if err != nil {
		c.JSON(401, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	api_token := syncUserAccounts(token.AccessToken); if api_token == "" {
		c.JSON(401, gin.H{
			"error": "Unable to sync user accounts",
		})
		return
	}

	c.JSON(200, gin.H{"access_token": api_token})
}

func Callback(c *gin.Context) {
	store, err := redistore.NewRediStore(10, "tcp", ":6379", "", []byte(os.Getenv("TP_SECRET"))); if err != nil {
		c.JSON(500, gin.H{
			"redis_error": err.Error(),
		})
		return
	}
	sessions, _ := store.Get(c.Request, "mysession")
	state := sessions.Values["state"]
	if state != c.Query("state") {
		c.JSON(401, gin.H{
			"error": "Invalid state",
		})
		return
	}
	redirect := fmt.Sprintf("%s?code=%s&state=%s",sessions.Values["redirect_uri"],c.Query("code"),state)
	c.Redirect(302,redirect)
}

func generateCodeVerifier() string {
    b := make([]byte, 32)
    rand.Read(b)
    return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
    s256 := sha256.Sum256([]byte(verifier))
    return base64.RawURLEncoding.EncodeToString(s256[:])
}

func buildSafeHostname(hostname string) string {
	return strings.ReplaceAll(hostname, ".", "_")
}

func syncUserAccounts(access_token string) string {
	provider := GetAuthProvider()
	us := &services.UserService{}
	info, err := provider.UserInfo(access_token); if err != nil {
		fmt.Println(err)
		return ""
	 }

	 user := us.FindByExternalID(fmt.Sprintf("%d", info.ID))
	 if user == nil {
		user = &entity.User{}
		user.Email = ""
		user.AuthorityID = fmt.Sprintf("%d", info.ID)
		user.Name = info.Name
		user.Role = roles.Editor
		user = us.Create(*user)
	 }

	 token, err := generateApiToken(user); if err != nil {
		fmt.Println(err)
		return ""
	}

	return token
}

func generateApiToken(user *entity.User) (string, error) {
	us := &services.UserService{}
	us.RemoveApiKeys(user.ID)
	token, err := jwt.GenerateJWT(user.ID.String(), user.Role); if err != nil {
		return "", err
	}
	key	:= &entity.ApiKeys{}
	key.Name = fmt.Sprintf("%s-apikey", user.Name)
	key.Token = config.HashSecret(token)
	key.Role = int(user.Role)
	key.UserID = user.ID
	us.CreateApiKey(*key)


	return token, nil
}