package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"
	"bufio"
)

// Config enthält die Konfiguration für das Programm
type Config struct {
	URL            string        `json:"url"`
	CheckInterval  time.Duration `json:"check_interval"`
	DataFile       string        `json:"data_file"`
	LemmyServer    string        `json:"lemmy_server"`
	LemmyCommunity string        `json:"lemmy_community"`
	LemmyUsername  string        `json:"lemmy_username"`
	LemmyPassword  string        `json:"lemmy_password"`
	LemmyToken     string        `json:"lemmy_token"`
	LemmyTokenExp  time.Time     `json:"lemmy_token_exp"`
	IgnoreDirs     []string      `json:"ignore_dirs"`

	// Mastodon-Konfiguration
	MastodonServer      string    `json:"mastodon_server"`
	MastodonAccessToken string    `json:"mastodon_access_token"` // Optional, wird überschrieben, wenn Username/Passwort genutzt wird
	MastodonUsername    string    `json:"mastodon_username"`
	MastodonPassword    string    `json:"mastodon_password"`
	MastodonClientID    string    `json:"mastodon_client_id"`
	MastodonClientSecret string   `json:"mastodon_client_secret"`
	MastodonToken       string    `json:"mastodon_token"`
	MastodonTokenExp    time.Time `json:"mastodon_token_exp"`
	MastodonVisibility  string    `json:"mastodon_visibility"` // z.B. "public", "unlisted", "private", "direct"
}

// LinkData speichert die gefundenen Links
type LinkData struct {
	Links       []string  `json:"links"`
	FailedLinks []string  `json:"failed_links"` // Links die beim Posten fehlgeschlagen sind
	LastSeen    time.Time `json:"last_seen"`
}

// LemmyLoginResponse ist die Antwortstruktur für den Lemmy-Login
type LemmyLoginResponse struct {
	Jwt    string `json:"jwt"`
	UserId int    `json:"id"`
}

// LemmyPostResponse ist die Antwortstruktur für die Lemmy-Post-Erstellung
type LemmyPostResponse struct {
	Post struct {
		Id int `json:"id"`
	} `json:"post_view"`
}

// DefaultConfig gibt die Standard-Konfiguration zurück
func DefaultConfig() Config {
	return Config{
		URL:            "http://www.grundstueckverkehrsgesetz.nrw.de",
		CheckInterval:  12 * time.Hour,
		DataFile:       "links.json",
		LemmyServer:    "https://natur.23.nu",
		LemmyCommunity: "kulturlandschaft",
		LemmyUsername:  "gvgbot",
		LemmyPassword:  "CHANGEME",
		LemmyToken:     "",
		LemmyTokenExp:  time.Time{},
		IgnoreDirs:     []string{"guetersloh"},

		MastodonServer:      "",
		MastodonAccessToken: "",
		MastodonUsername:    "",
		MastodonPassword:    "",
		MastodonClientID:    "",
		MastodonClientSecret: "",
		MastodonToken:       "",
		MastodonTokenExp:    time.Time{},
		MastodonVisibility:  "unlisted",
	}
}

// loadConfig lädt die Konfiguration aus einer JSON-Datei oder erstellt eine Standard-Konfiguration
func loadConfig(configFile string) (Config, error) {
	config := DefaultConfig()

	if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err == nil {
			err = json.Unmarshal(data, &config)
			if err != nil {
				return config, fmt.Errorf("Fehler beim Parsen der Konfigurationsdatei: %v", err)
			}
		}
	}

	return config, nil
}

// saveConfig speichert die Konfiguration in eine JSON-Datei
func saveConfig(config Config, configFile string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("Fehler beim Marshalling der Konfiguration: %v", err)
	}

	return os.WriteFile(configFile, data, 0644)
}

// loadLinkData lädt die gespeicherten Link-Daten
func loadLinkData(filename string) (LinkData, error) {
	var data LinkData

	file, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// Datei existiert nicht, erstelle leere Daten
			return LinkData{
				Links:       []string{},
				FailedLinks: []string{},
				LastSeen:    time.Now(),
			}, nil
		}
		return data, fmt.Errorf("Fehler beim Lesen der Link-Datei: %v", err)
	}

	err = json.Unmarshal(file, &data)
	if err != nil {
		return data, fmt.Errorf("Fehler beim Parsen der Link-Datei: %v", err)
	}

	// Stelle sicher, dass FailedLinks initialisiert ist (für alte Dateien)
	if data.FailedLinks == nil {
		data.FailedLinks = []string{}
	}

	return data, nil
}

// saveLinkData speichert die Link-Daten
func saveLinkData(data LinkData, filename string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("Fehler beim Marshalling der Link-Daten: %v", err)
	}

	return os.WriteFile(filename, jsonData, 0644)
}

// fetchURL ruft eine URL ab und gibt den HTML-Inhalt zurück
func fetchURL(url string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("Fehler beim Abrufen der URL %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP-Fehler %d für URL %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Fehler beim Lesen der Antwort: %v", err)
	}

	return string(body), nil
}

// extractLinks extrahiert alle Links aus dem HTML-Inhalt
func extractLinks(htmlContent string, ignoreDirs []string) ([]string, error) {
	doc, err := htmlquery.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("Fehler beim Parsen des HTML: %v", err)
	}

	// XPath-Abfrage für alle Links
	nodes, err := htmlquery.QueryAll(doc, "//a[@href]")
	if err != nil {
		return nil, fmt.Errorf("Fehler bei XPath-Abfrage: %v", err)
	}

	var links []string
	for _, node := range nodes {
		href := htmlquery.SelectAttr(node, "href")
		if href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "javascript:") {
			// Nur Links zu Unterverzeichnissen mit index.htm filtern
			if strings.Contains(href, "/") && strings.HasSuffix(href, "/index.htm") {
				// Verzeichnisname extrahieren
				parts := strings.Split(href, "/")
				if len(parts) > 1 {
					dir := parts[0]
					ignore := false
					for _, ign := range ignoreDirs {
						if strings.EqualFold(dir, ign) {
							ignore = true
							break
						}
					}
					if ignore {
						continue
					}
				}
				links = append(links, href)
			}
		}
	}

	return links, nil
}

// findNewLinks findet neue Links im Vergleich zu den gespeicherten
func findNewLinks(currentLinks, savedLinks, failedLinks []string) []string {
	savedMap := make(map[string]bool)
	for _, link := range savedLinks {
		savedMap[link] = true
	}

	var newLinks []string
	for _, link := range currentLinks {
		if !savedMap[link] {
			newLinks = append(newLinks, link)
		}
	}

	// Füge fehlgeschlagene Links hinzu, die erneut versucht werden sollen
	for _, link := range failedLinks {
		newLinks = append(newLinks, link)
	}

	return newLinks
}

// findRemovedLinks findet Links, die nicht mehr auf der Website erscheinen
func findRemovedLinks(currentLinks, savedLinks []string) []string {
	currentMap := make(map[string]bool)
	for _, link := range currentLinks {
		currentMap[link] = true
	}

	var removedLinks []string
	for _, link := range savedLinks {
		if !currentMap[link] {
			removedLinks = append(removedLinks, link)
		}
	}

	return removedLinks
}

// extractTextBetweenHR extrahiert den Text zwischen den ersten beiden <hr>-Tags aus HTML
func extractTextBetweenHR(htmlContent string) (string, string, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("fehler beim Parsen des HTML: %v", err)
	}

	var title string
	var textContent strings.Builder
	var hrCount int
	var inSection bool

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "hr" {
			hrCount++
			if hrCount == 1 {
				inSection = true
				return
			} else if hrCount == 2 {
				inSection = false
				return
			}
		}

		if inSection {
			if n.Type == html.ElementNode && n.Data == "h3" {
				// Überschrift extrahieren
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						title += c.Data
					}
				}
			} else if n.Type == html.ElementNode && (n.Data == "strong" || n.Data == "b") {
				// Fett-Text extrahieren
				textContent.WriteString("**")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						textContent.WriteString(c.Data)
					}
				}
				textContent.WriteString("**")
			} else if n.Type == html.ElementNode && (n.Data == "em" || n.Data == "i") {
				// Kursiv-Text extrahieren
				textContent.WriteString("*")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						textContent.WriteString(c.Data)
					}
				}
				textContent.WriteString("*")
			} else if n.Type == html.ElementNode && n.Data == "br" {
				textContent.WriteString("\n")
			} else if n.Type == html.ElementNode && n.Data == "p" {
				textContent.WriteString("\n\n")
			} else if n.Type == html.TextNode {
				// Nur Text extrahieren, wenn es nicht in einem bereits verarbeiteten Tag ist
				parent := n.Parent
				if parent != nil && parent.Type == html.ElementNode {
					if parent.Data != "strong" && parent.Data != "b" && parent.Data != "em" && parent.Data != "i" {
						textContent.WriteString(n.Data)
					}
				} else {
					textContent.WriteString(n.Data)
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	text := strings.TrimSpace(textContent.String())
	title = strings.TrimSpace(title)

	// Standard-Formularzeile entfernen
	text = strings.ReplaceAll(text, "Erwerbsinteressierte Landwirtinnen und Landwirte können ihr Erwerbsinteresse mit dem unten stehenden Formular bekunden.", "")
	text = strings.ReplaceAll(text, "  ", " ")
	text = strings.TrimSpace(text)

	return title, text, nil
}

// truncateString kürzt einen String auf die angegebene Länge
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractCityName extrahiert den Stadtnamen aus der Detailseite
func extractCityName(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}

	var cityName string
	var f func(*html.Node)
	f = func(n *html.Node) {
		// 1. Suche nach h1-Tag (Hauptüberschrift)
		if n.Type == html.ElementNode && n.Data == "h1" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					cityName = strings.TrimSpace(c.Data)
					return
				}
			}
		}

		// 2. Fallback: Suche nach "Sie sind hier:" Zeile
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if strings.Contains(text, "Sie sind hier:") {
				// Suche nach dem letzten ">" um den Stadtnamen zu finden
				parts := strings.Split(text, ">")
				if len(parts) > 1 {
					cityName = strings.TrimSpace(parts[len(parts)-1])
					return
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	return cityName
}

// checkWebsite überprüft die Website auf neue Links
func checkWebsite(config Config, testMode bool) error {
	log.Printf("Überprüfe Website: %s", config.URL)

	// HTML-Inhalt abrufen
	htmlContent, err := fetchURL(config.URL)
	if err != nil {
		return err
	}

	// Links extrahieren
	currentLinks, err := extractLinks(htmlContent, config.IgnoreDirs)
	if err != nil {
		return err
	}

	log.Printf("Gefundene Links: %d", len(currentLinks))

	// Gespeicherte Links laden
	savedData, err := loadLinkData(config.DataFile)
	if err != nil {
		return err
	}

	// Neue Links finden (inklusive fehlgeschlagene Links)
	newLinks := findNewLinks(currentLinks, savedData.Links, savedData.FailedLinks)
	
	// Logge fehlgeschlagene Links, die erneut versucht werden
	if len(savedData.FailedLinks) > 0 {
		log.Printf("🔄 Fehlgeschlagene Links werden erneut versucht (%d):", len(savedData.FailedLinks))
		for i, link := range savedData.FailedLinks {
			log.Printf("  %d. %s", i+1, link)
		}
	}
	// Entfernte Links finden
	removedLinks := findRemovedLinks(currentLinks, savedData.Links)

	// Lemmy-Login nur durchführen, wenn neue Links gefunden wurden
	var jwt string
	var communityID int
	if len(newLinks) > 0 {
		log.Printf("🚨 NEUE LINKS GEFUNDEN (%d):", len(newLinks))
		
		// Fehlgeschlagene Links für diesen Durchgang zurücksetzen
		savedData.FailedLinks = []string{}

		// Lemmy-Login nur einmal pro Check durchführen
		if config.LemmyToken != "" && time.Now().Before(config.LemmyTokenExp) {
			// Verwende gespeichertes Token
			jwt = config.LemmyToken
			log.Printf("Verwende gespeichertes Lemmy-Token (gültig bis %v)", config.LemmyTokenExp)
		} else {
			// Hole neues Token
			jwt, err = lemmyLogin(config.LemmyServer, config.LemmyUsername, config.LemmyPassword)
			if err != nil {
				log.Printf("Fehler beim Lemmy-Login: %v", err)
				jwt = ""
			} else {
				// Token für 1 Stunde speichern
				config.LemmyToken = jwt
				config.LemmyTokenExp = time.Now().Add(1 * time.Hour)
				log.Printf("Neues Lemmy-Token geholt und gespeichert (gültig bis %v)", config.LemmyTokenExp)
			}
		}

		// Community-ID für neue Links abfragen
		if jwt != "" {
			communityID, err = lemmyGetCommunityID(config.LemmyServer, jwt, config.LemmyCommunity)
			if err != nil {
				log.Printf("Fehler beim Abrufen der Community-ID: %v", err)
				communityID = 0
			} else {
				log.Printf("Community-ID für '%s': %d", config.LemmyCommunity, communityID)
			}
		}

		for i, link := range newLinks {
			log.Printf("  %d. %s", i+1, link)

			// Detailseite abrufen und Text extrahieren
			pageURL := config.URL
			if !strings.HasSuffix(pageURL, "/") {
				pageURL += "/"
			}
			pageURL += link
			log.Printf("    Abrufe Detailseite: %s", pageURL)
			pageContent, err := fetchURL(pageURL)
			if err != nil {
				log.Printf("    Fehler beim Abrufen der Detailseite %s: %v", pageURL, err)
				continue
			}
			log.Printf("    Detailseite erfolgreich abgerufen, Länge: %d Zeichen", len(pageContent))
			extractedTitle, text, err := extractTextBetweenHR(pageContent)
			if err != nil {
				log.Printf("    Fehler beim Extrahieren des Textes aus %s: %v", pageURL, err)
				continue
			}
			log.Printf("    Text extrahiert, Länge: %d Zeichen", len(text))

			// Stadtnamen extrahieren
			cityName := extractCityName(pageContent)
			if cityName != "" {
				log.Printf("    Stadtnamen extrahiert: %s", cityName)
			}

			if text != "" {
				log.Printf("--- Auszug aus %s ---\n%s\n--------------------------", link, text)

				// --- NEU: Plattform-Checks ---
				lemmyConfigured := config.LemmyServer != "" && config.LemmyCommunity != "" && config.LemmyUsername != "" && config.LemmyPassword != ""
				mastodonConfigured := config.MastodonServer != "" && config.MastodonAccessToken != ""

				if !lemmyConfigured && !mastodonConfigured {
					log.Printf("    ❌ Weder Lemmy noch Mastodon sind konfiguriert. Link wird nicht als erledigt markiert.")
					savedData.FailedLinks = append(savedData.FailedLinks, link)
					continue
				}

				var postErrs []string
				lemmySuccess := true
				mastodonSuccess := true

				// --- Lemmy Post ---
				if lemmyConfigured {
					title := cityName + ": Grundstücksverkauf an Nicht-LandwirtIn"
					if cityName == "" {
						title = strings.Title(strings.Split(link, "/")[0]) + ": Grundstücksverkauf an Nicht-LandwirtIn"
					}
					if extractedTitle != "" {
						title += " " + extractedTitle
					}
					if !testMode {
						if jwt != "" {
							err = lemmyCreatePost(config.LemmyServer, jwt, communityID, title, text, pageURL)
							if err != nil {
								log.Printf("    ❌ Fehler beim Erstellen des Lemmy-Posts: %v", err)
								lemmySuccess = false
								postErrs = append(postErrs, "Lemmy: "+err.Error())
							} else {
								log.Printf("    ✅ Lemmy-Post erfolgreich erstellt für %s", link)
							}
						} else {
							log.Printf("    ❌ Kein gültiges Lemmy-Token, Lemmy-Post übersprungen.")
							lemmySuccess = false
							postErrs = append(postErrs, "Lemmy: Kein gültiges Token")
						}
					} else {
						log.Printf("🧪 TEST: Lemmy-Post würde erstellt werden:")
						log.Printf("    Server: %s", config.LemmyServer)
						log.Printf("    Community: %s (ID: %d)", config.LemmyCommunity, communityID)
						log.Printf("    URL: %s", pageURL)
						log.Printf("    Titel: %s", title)
						log.Printf("    Text (erste 200 Zeichen): %s", truncateString(text, 200))
						if len(text) > 200 {
							log.Printf("    ... (Text ist %d Zeichen lang)", len(text))
						}
						log.Printf("    Vollständiger Text:")
						log.Printf("    ---")
						log.Printf("%s", text)
						log.Printf("    ---")
					}
				}

				// --- Mastodon Post ---
				if mastodonConfigured {
					// Token-Handling wie bei Lemmy
					mastodonToken := config.MastodonAccessToken
					if mastodonToken == "" || (config.MastodonToken != "" && time.Now().After(config.MastodonTokenExp)) {
						if config.MastodonUsername != "" && config.MastodonPassword != "" && config.MastodonClientID != "" && config.MastodonClientSecret != "" {
							log.Printf("    Mastodon: Hole neues Access Token per Passwort...")
							token, exp, err := mastodonLogin(config.MastodonServer, config.MastodonClientID, config.MastodonClientSecret, config.MastodonUsername, config.MastodonPassword)
							if err != nil {
								log.Printf("    ❌ Fehler beim Mastodon-Login: %v", err)
								mastodonSuccess = false
								postErrs = append(postErrs, "Mastodon-Login: "+err.Error())
							} else {
								mastodonToken = token
								config.MastodonToken = token
								config.MastodonTokenExp = exp
								log.Printf("    Mastodon: Neues Token geholt und gespeichert (gültig bis %v)", exp)
							}
						}
					}
					if mastodonToken == "" {
						if config.MastodonUsername != "" || config.MastodonPassword != "" || config.MastodonClientID != "" || config.MastodonClientSecret != "" {
							log.Printf("    ❌ Mastodon: Kein Access Token verfügbar und Login mit Username/Passwort/ClientID/Secret nicht möglich (z.B. GoToSocial). Bitte ein App-Passwort (mastodon_access_token) verwenden.")
						}
						log.Printf("    ❌ Kein Mastodon-Token verfügbar, Mastodon-Post übersprungen.")
						mastodonSuccess = false
						postErrs = append(postErrs, "Mastodon: Kein Token")
					} else if !testMode {
						mastodonText := text
						if cityName != "" {
							mastodonText = cityName + ": Grundstücksverkauf an Nicht-LandwirtIn\n" + text
						}
						err = mastodonCreatePost(config.MastodonServer, mastodonToken, mastodonText, config.MastodonVisibility)
						if err != nil {
							log.Printf("    ❌ Fehler beim Erstellen des Mastodon-Posts: %v", err)
							mastodonSuccess = false
							postErrs = append(postErrs, "Mastodon: "+err.Error())
						} else {
							log.Printf("    ✅ Mastodon-Post erfolgreich erstellt für %s", link)
						}
					} else if testMode {
						mastodonText := text
						if cityName != "" {
							mastodonText = cityName + ": Grundstücksverkauf an Nicht-LandwirtIn\n" + text
						}
						log.Printf("🧪 TEST: Mastodon-Post würde erstellt werden:")
						log.Printf("    Server: %s", config.MastodonServer)
						log.Printf("    Sichtbarkeit: %s", config.MastodonVisibility)
						log.Printf("    Text (erste 200 Zeichen): %s", truncateString(mastodonText, 200))
						if len(mastodonText) > 200 {
							log.Printf("    ... (Text ist %d Zeichen lang)", len(mastodonText))
						}
						log.Printf("    Vollständiger Text:")
						log.Printf("    ---")
						log.Printf("%s", mastodonText)
						log.Printf("    ---")
					}
				}

				if (lemmyConfigured && !lemmySuccess) || (mastodonConfigured && !mastodonSuccess) {
					log.Printf("    ❌ Mindestens ein Post fehlgeschlagen (%s). Link wird erneut versucht.", strings.Join(postErrs, "; "))
					savedData.FailedLinks = append(savedData.FailedLinks, link)
				} else {
					log.Printf("    ✅ Link erfolgreich auf allen konfigurierten Plattformen gepostet: %s", link)
					savedData.Links = append(savedData.Links, link)
				}
			} else {
				log.Printf("    Kein Text zwischen <hr>-Tags gefunden")
			}
		}
	}

	if len(removedLinks) > 0 {
		log.Printf("🗑️  ENTFERNTE LINKS (%d):", len(removedLinks))
		for i, link := range removedLinks {
			log.Printf("  %d. %s", i+1, link)
		}
		
		// Entferne gelöschte Links aus der gespeicherten Liste
		currentMap := make(map[string]bool)
		for _, link := range currentLinks {
			currentMap[link] = true
		}
		
		var updatedLinks []string
		for _, link := range savedData.Links {
			if currentMap[link] {
				updatedLinks = append(updatedLinks, link)
			}
		}
		savedData.Links = updatedLinks
	}

	if len(newLinks) == 0 && len(removedLinks) == 0 {
		log.Printf("Keine Änderungen gefunden")
	}

	// Aktuelle Links speichern (nur erfolgreich gepostete Links bleiben in der Liste)
	// Entfernte Links werden automatisch entfernt, da sie nicht mehr in currentLinks sind
	savedData.LastSeen = time.Now()

	err = saveLinkData(savedData, config.DataFile)
	if err != nil {
		return fmt.Errorf("Fehler beim Speichern der Link-Daten: %v", err)
	}

	// Konfiguration mit Token speichern
	err = saveConfig(config, "config.json")
	if err != nil {
		log.Printf("Warnung: Konfiguration konnte nicht gespeichert werden: %v", err)
	}

	return nil
}

// runMonitoring startet die kontinuierliche Überwachung
func runMonitoring(ctx context.Context, config Config, testMode bool) error {
	log.Printf("Starte Überwachung der Website: %s", config.URL)
	log.Printf("Überprüfungsintervall: %v", config.CheckInterval)
	log.Printf("Datendatei: %s", config.DataFile)

	// Erste Überprüfung sofort durchführen
	err := checkWebsite(config, testMode)
	if err != nil {
		log.Printf("Fehler bei der ersten Überprüfung: %v", err)
	}

	// Timer für regelmäßige Überprüfungen
	ticker := time.NewTicker(config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Überwachung beendet")
			return nil
		case <-ticker.C:
			err := checkWebsite(config, testMode)
			if err != nil {
				log.Printf("Fehler bei der Website-Überprüfung: %v", err)
			}
		}
	}
}

func lemmyLogin(serverURL, username, password string) (string, error) {
	loginUrl := serverURL + "/api/v3/user/login"
	payload := map[string]string{
		"username_or_email": username,
		"password":          password,
	}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(loginUrl, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("Lemmy-Login fehlgeschlagen: %v", err)
	}
	defer resp.Body.Close()

	// Komplette Antwort lesen
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Fehler beim Lesen der Login-Antwort: %v", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Lemmy-Login HTTP %d - Antwort: %s", resp.StatusCode, string(body))
	}

	var loginResp LemmyLoginResponse
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return "", fmt.Errorf("Lemmy-Login JSON-Fehler: %v - Antwort: %s", err, string(body))
	}
	return loginResp.Jwt, nil
}

// Hilfsfunktion, um Community-ID anhand des Namens zu holen
func lemmyGetCommunityID(serverURL, jwt, communityName string) (int, error) {
	url := serverURL + "/api/v3/community?name=" + communityName
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("Community-GET HTTP %d", resp.StatusCode)
	}
	var respData struct {
		CommunityView struct {
			Community struct {
				Id int `json:"id"`
			} `json:"community"`
		} `json:"community_view"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return 0, err
	}
	return respData.CommunityView.Community.Id, nil
}

// Passe lemmyCreatePost an, damit sie community_id verwendet
func lemmyCreatePost(serverURL, jwt string, communityID int, title, body, url string) error {
	postUrl := serverURL + "/api/v3/post"
	payload := map[string]interface{}{
		"name":         title,
		"body":         body,
		"url":          url,
		"community_id": communityID,
	}
	data, _ := json.Marshal(payload)
	client := &http.Client{}
	req, err := http.NewRequest("POST", postUrl, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Post-Erstellung HTTP %d - Antwort: %s", resp.StatusCode, string(body))
	}
	log.Printf("Post-Erstellung %s HTTP %d - Antwort: %s", payload, resp.StatusCode, string(body))
	return nil
}

// mastodonLogin holt ein Access Token per OAuth2 Password Grant
func mastodonLogin(server, clientID, clientSecret, username, password string) (string, time.Time, error) {
	tokenURL := server + "/oauth/token"
	payload := url.Values{}
	payload.Set("grant_type", "password")
	payload.Set("client_id", clientID)
	payload.Set("client_secret", clientSecret)
	payload.Set("username", username)
	payload.Set("password", password)
	payload.Set("scope", "read write")

	resp, err := http.PostForm(tokenURL, payload)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("Mastodon-Login fehlgeschlagen: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("Fehler beim Lesen der Mastodon-Login-Antwort: %v", err)
	}
	if resp.StatusCode != 200 {
		return "", time.Time{}, fmt.Errorf("Mastodon-Login HTTP %d - Antwort: %s", resp.StatusCode, string(body))
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("Mastodon-Login JSON-Fehler: %v - Antwort: %s", err, string(body))
	}
	exp := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return tokenResp.AccessToken, exp, nil
}

// mastodonCreatePost erstellt einen neuen Beitrag auf Mastodon
func mastodonCreatePost(server, token, text, visibility string) error {
	apiUrl := server + "/api/v1/statuses"
	payload := map[string]interface{}{
		"status":     text,
		"visibility": visibility,
	}
	data, _ := json.Marshal(payload)
	client := &http.Client{}
	req, err := http.NewRequest("POST", apiUrl, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Mastodon-Post HTTP %d - Antwort: %s", resp.StatusCode, string(body))
	}
	return nil
}

func savePostAsJSON(title, markdown, url, community string) error {
	post := map[string]interface{}{
		"title":     title,
		"markdown":  markdown,
		"url":       url,
		"community": community,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	if err := os.MkdirAll("posts", 0755); err != nil {
		return err
	}
	filename := filepath.Join("posts", fmt.Sprintf("%s_%d.json", strings.ReplaceAll(title, " ", "_"), time.Now().UnixNano()))
	data, err := json.MarshalIndent(post, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func printMastodonAuthURL(config Config) {
	if config.MastodonServer == "" || config.MastodonClientID == "" {
		fmt.Println("mastodon_server und mastodon_client_id müssen in der Konfiguration gesetzt sein.")
		return
	}
	url := fmt.Sprintf("%soauth/authorize?client_id=%s&redirect_uri=urn:ietf:wg:oauth:2.0:oob&response_type=code&scope=write", config.MastodonServer, config.MastodonClientID)
	fmt.Println("Öffne folgende URL im Browser, um den Authorization Code zu erhalten:")
	fmt.Println(url)
}

func obtainMastodonTokenInteractive(config *Config) error {
	if config.MastodonServer == "" || config.MastodonClientID == "" || config.MastodonClientSecret == "" {
		return fmt.Errorf("mastodon_server, mastodon_client_id und mastodon_client_secret müssen gesetzt sein")
	}
	redirectURI := "urn:ietf:wg:oauth:2.0:oob"
	scope := "write"

	authURL := fmt.Sprintf("%soauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=%s", config.MastodonServer, config.MastodonClientID, redirectURI, scope)
	fmt.Println("Bitte öffne folgende URL im Browser, logge dich ein und erlaube den Zugriff:")
	fmt.Println(authURL)
	fmt.Print("Gib den angezeigten Code ein: ")
	reader := bufio.NewReader(os.Stdin)
	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("Kein Code eingegeben")
	}

	// Tausche Code gegen Access Token
	payload := map[string]string{
		"redirect_uri": redirectURI,
		"client_id": config.MastodonClientID,
		"client_secret": config.MastodonClientSecret,
		"grant_type": "authorization_code",
		"code": code,
	}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(config.MastodonServer+"oauth/token", "application/json", strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("Fehler beim Token-Austausch: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("Token-Austausch fehlgeschlagen: %s", string(body))
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("Fehler beim Parsen der Token-Antwort: %v", err)
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("Kein Access Token erhalten")
	}
	config.MastodonAccessToken = tokenResp.AccessToken
	config.MastodonToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		config.MastodonTokenExp = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	fmt.Println("Access Token erfolgreich erhalten und gespeichert.")
	return saveConfig(*config, "config.json")
}

func main() {
	// Command line flags
	var loopMode = flag.Bool("loop", false, "Run in continuous monitoring mode")
	var testMode = flag.Bool("test", false, "Run in test mode - don't post to Lemmy, just show what would be posted")
	flag.Parse()

	// Konfiguration laden
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Fehler beim Laden der Konfiguration: %v", err)
	}

	// Mastodon OAuth2-Flow automatisch durchführen, wenn kein Token vorhanden ist, aber Server und ClientID/Secret gesetzt sind
	if config.MastodonServer != "" && config.MastodonClientID != "" && config.MastodonClientSecret != "" && config.MastodonAccessToken == "" && config.MastodonToken == "" {
		err := obtainMastodonTokenInteractive(&config)
		if err != nil {
			log.Fatalf("Fehler beim Mastodon-OAuth2-Flow: %v", err)
		}
		// Nach erfolgreichem Token-Erhalt: Programm normal fortsetzen
	}

	if *loopMode {
		// Kontinuierliche Überwachung
		log.Printf("Starte kontinuierliche Überwachung...")

		// Kontext für graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Signal-Handler für graceful shutdown
		go func() {
			sigChan := make(chan os.Signal, 1)
			// signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			<-sigChan
			log.Println("Shutdown-Signal empfangen...")
			cancel()
		}()

		// Überwachung starten
		err = runMonitoring(ctx, config, *testMode)
		if err != nil {
			log.Fatalf("Fehler in der Überwachung: %v", err)
		}
	} else {
		// Einmalige Überprüfung
		log.Printf("Führe einmalige Überprüfung durch...")
		err = checkWebsite(config, *testMode)
		if err != nil {
			log.Fatalf("Fehler bei der Website-Überprüfung: %v", err)
		}
		log.Printf("Überprüfung abgeschlossen.")
	}
}
