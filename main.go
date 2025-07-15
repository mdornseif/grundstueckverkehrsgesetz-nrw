package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"
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
				if jwt != "" {
					title := cityName + ": Grundstücksverkauf an Nicht-LandwirtIn"
					if cityName == "" {
						title = strings.Title(strings.Split(link, "/")[0]) + ": Grundstücksverkauf an Nicht-LandwirtIn"
					}

					// Überschrift zum Titel hinzufügen, falls vorhanden
					if extractedTitle != "" {
						title += " " + extractedTitle
					}

					if !testMode {
						err = lemmyCreatePost(config.LemmyServer, jwt, communityID, title, text, pageURL)
						if err != nil {
							log.Printf("    ❌ Fehler beim Erstellen des Lemmy-Posts: %v", err)
							log.Printf("    🔄 Link wird beim nächsten Durchgang erneut versucht: %s", link)
							savedData.FailedLinks = append(savedData.FailedLinks, link)
						} else {
							log.Printf("    ✅ Lemmy-Post erfolgreich erstellt für %s", link)
							savedData.Links = append(savedData.Links, link) // Nur erfolgreiche Links speichern
						}
					} else {
						// Test-Modus: Zeige was gepostet werden würde
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

	// Konfiguration speichern (falls sie nicht existierte)
	err = saveConfig(config, "config.json")
	if err != nil {
		log.Printf("Warnung: Konfiguration konnte nicht gespeichert werden: %v", err)
	}

	if *testMode {
		log.Printf("🧪 TEST-MODUS: Keine Posts werden an Lemmy gesendet!")
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
