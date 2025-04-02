package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ------------------- Partie Kraken API -------------------

// Structure pour récupérer le statut et le timing du serveur
type ServerTime struct {
	Unixtime int64  `json:"unixtime"`
	RFC1123  string `json:"rfc1123"`
}

// GetServerStatus récupère le statut et le timing du serveur Kraken
func GetServerStatus() (*ServerTime, error) {
	url := "https://api.kraken.com/0/public/Time"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response struct {
		Error  []string   `json:"error"`
		Result ServerTime `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	if len(response.Error) > 0 {
		return nil, fmt.Errorf("API Time error: %v", response.Error)
	}
	return &response.Result, nil
}

// Structure pour récupérer les Asset Pairs
type AssetPair struct {
	AltName string `json:"altname"`
	WSName  string `json:"wsname"`
}

// Structure pour maintenir la correspondance entre les noms de paires
type PairMapping struct {
	InternalName string
	AltName      string
	Volume       float64
}

// Structure pour stocker une paire et son volume
type PairVolume struct {
	Pair   string
	Volume float64
}

// GetTopVolumeAssetPairs récupère les paires avec le plus grand volume d'échanges
func GetTopVolumeAssetPairs(count int) ([]PairMapping, error) {
	// 1. Récupérer toutes les paires disponibles
	url := "https://api.kraken.com/0/public/AssetPairs"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response struct {
		Error  []string             `json:"error"`
		Result map[string]AssetPair `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	if len(response.Error) > 0 {
		return nil, fmt.Errorf("API AssetPairs error: %v", response.Error)
	}

	// 2. Préparer des batchs de paires pour les requêtes Ticker et garder la correspondance
	var pairsMapping []PairMapping

	for internalName, pair := range response.Result {
		if pair.AltName != "" {
			pairsMapping = append(pairsMapping, PairMapping{
				InternalName: internalName,
				AltName:      pair.AltName,
				Volume:       0,
			})
		}
	}

	// 3. Faire des requêtes par lots pour éviter de surcharger l'API
	batchSize := 10 // Kraken permet jusqu'à environ 20 paires par requête
	for i := 0; i < len(pairsMapping); i += batchSize {
		end := i + batchSize
		if end > len(pairsMapping) {
			end = len(pairsMapping)
		}

		var batchNames []string
		for j := i; j < end; j++ {
			batchNames = append(batchNames, pairsMapping[j].InternalName)
		}

		pairParam := strings.Join(batchNames, ",")

		// Récupérer le ticker pour ce lot de paires
		tickerURL := fmt.Sprintf("https://api.kraken.com/0/public/Ticker?pair=%s", pairParam)
		tickerResp, err := http.Get(tickerURL)
		if err != nil {
			log.Printf("Erreur lors de la récupération des tickers: %v", err)
			continue
		}

		var tickerResponse TickerResponse
		if err := json.NewDecoder(tickerResp.Body).Decode(&tickerResponse); err != nil {
			tickerResp.Body.Close()
			log.Printf("Erreur lors du décodage des tickers: %v", err)
			continue
		}
		tickerResp.Body.Close()

		if len(tickerResponse.Error) > 0 {
			log.Printf("API Ticker error: %v", tickerResponse.Error)
			continue
		}

		// Traiter les résultats du ticker
		for internalName, ticker := range tickerResponse.Result {
			volume, _ := strconv.ParseFloat(ticker.Volume[1], 64) // Volume sur 24h

			// Mettre à jour le volume dans notre correspondance
			for j := range pairsMapping {
				if pairsMapping[j].InternalName == internalName {
					pairsMapping[j].Volume = volume
					break
				}
			}
		}

		// Attendre un peu pour respecter les limites de l'API
		time.Sleep(200 * time.Millisecond)
	}

	// 4. Trier les paires par volume décroissant
	sort.Slice(pairsMapping, func(i, j int) bool {
		return pairsMapping[i].Volume > pairsMapping[j].Volume
	})

	// 5. Sélectionner les N premières paires
	var topPairs []PairMapping
	for i := 0; i < count && i < len(pairsMapping); i++ {
		pair := pairsMapping[i]
		topPairs = append(topPairs, pair)
		log.Printf("Paire #%d: %s (Nom interne: %s, Volume: %.2f)",
			i+1, pair.AltName, pair.InternalName, pair.Volume)
	}

	return topPairs, nil
}

// Modifier la fonction GetAssetPairs pour utiliser notre nouvelle fonction
func GetAssetPairs() ([]PairMapping, error) {
	return GetTopVolumeAssetPairs(20)
}

// Structures pour récupérer les informations du Ticker
type TickerInfo struct {
	Ask    []string `json:"a"` // Prix de vente
	Bid    []string `json:"b"` // Prix d'achat
	Last   []string `json:"c"` // Dernier trade
	Volume []string `json:"v"` // Volume sur 24h
	High   []string `json:"h"` // Prix le plus haut sur 24h
	Low    []string `json:"l"` // Prix le plus bas sur 24h
}

type TickerResponse struct {
	Error  []string              `json:"error"`
	Result map[string]TickerInfo `json:"result"`
}

func GetTicker(pair string) (*TickerInfo, error) {
	url := fmt.Sprintf("https://api.kraken.com/0/public/Ticker?pair=%s", pair)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tickerResp TickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&tickerResp); err != nil {
		return nil, err
	}
	if len(tickerResp.Error) > 0 {
		return nil, fmt.Errorf("API Ticker error: %v", tickerResp.Error)
	}
	for _, info := range tickerResp.Result {
		return &info, nil
	}
	return nil, fmt.Errorf("No ticker data found for %s", pair)
}

// ------------------- Partie Base de Données SQLite -------------------

// InitDB ouvre (ou crée) la base SQLite et crée la table si nécessaire.
func InitDB(dbPath string) *sql.DB {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS crypto_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pair TEXT UNIQUE,
		ask_price REAL,
		bid_price REAL,
		last_trade_price REAL,
		volume REAL,
		high_price REAL,
		low_price REAL,
		timestamp DATETIME
	);`
	if _, err = db.Exec(createTableQuery); err != nil {
		log.Fatal(err)
	}
	return db
}

// CleanDB vide la base pour repartir sur des données réelles uniquement.
func CleanDB(db *sql.DB) {
	_, err := db.Exec("DELETE FROM crypto_data")
	if err != nil {
		log.Println("Erreur lors du nettoyage de la base:", err)
	} else {
		log.Println("Base nettoyée avec succès.")
	}
}

// InsertCryptoData stocke les informations d'une paire dans la base.
func InsertCryptoData(db *sql.DB, pair string, ask, bid, lastTrade, volume, high, low float64, timestamp string) {
	query := `INSERT INTO crypto_data (pair, ask_price, bid_price, last_trade_price, volume, high_price, low_price, timestamp) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	          ON CONFLICT(pair) DO UPDATE SET 
	            ask_price=excluded.ask_price, 
	            bid_price=excluded.bid_price, 
	            last_trade_price=excluded.last_trade_price,
	            volume=excluded.volume,
	            high_price=excluded.high_price,
	            low_price=excluded.low_price,
	            timestamp=excluded.timestamp;`

	_, err := db.Exec(query, pair, ask, bid, lastTrade, volume, high, low, timestamp)
	if err != nil {
		log.Println("Erreur lors de l'insertion des données pour", pair, ":", err)
	}
}

// ------------------- Partie Export CSV -------------------

// Génère un nom de fichier normalisé pour le CSV
func generateCSVFilename() string {
	now := time.Now()
	// Arrondir à la tranche de 5 minutes la plus proche
	minutes := now.Minute()
	minutes = (minutes / 5) * 5

	return fmt.Sprintf("crypto_data_%02d_%02d_%d_%02d_%02d.csv",
		now.Day(), now.Month(), now.Year(), now.Hour(), minutes)
}

// Crée le dossier pour stocker les fichiers CSV s'il n'existe pas
func initCSVDirectory() string {
	csvDir := "data/csv"
	if _, err := os.Stat(csvDir); os.IsNotExist(err) {
		os.MkdirAll(csvDir, 0755)
	}
	return csvDir
}

// ExportAllPairsToSingleCSV exporte toutes les paires vers un seul fichier CSV
func ExportAllPairsToSingleCSV(db *sql.DB) (string, error) {
	csvDir := initCSVDirectory()
	filename := generateCSVFilename()
	filePath := filepath.Join(csvDir, filename)

	// Récupérer les données de toutes les paires
	rows, err := db.Query(
		"SELECT pair, ask_price, bid_price, last_trade_price, volume, high_price, low_price, timestamp FROM crypto_data",
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	// Créer le fichier CSV
	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Écrire l'en-tête
	headers := []string{"Pair", "Ask", "Bid", "Last", "Volume", "High", "Low", "Timestamp"}
	if err := writer.Write(headers); err != nil {
		return "", err
	}

	// Écrire les données pour toutes les paires
	for rows.Next() {
		var pair string
		var ask, bid, lastTrade, volume, high, low float64
		var timestamp string
		if err := rows.Scan(&pair, &ask, &bid, &lastTrade, &volume, &high, &low, &timestamp); err != nil {
			return "", err
		}

		record := []string{
			pair,
			strconv.FormatFloat(ask, 'f', 8, 64),      // Augmenté de 4 à 8 décimales
			strconv.FormatFloat(bid, 'f', 8, 64),      // Augmenté de 4 à 8 décimales
			strconv.FormatFloat(lastTrade, 'f', 8, 64), // Augmenté de 4 à 8 décimales
			strconv.FormatFloat(volume, 'f', 4, 64),    // Volume reste à 4 décimales car c'est moins critique
			strconv.FormatFloat(high, 'f', 8, 64),      // Augmenté de 4 à 8 décimales
			strconv.FormatFloat(low, 'f', 8, 64),       // Augmenté de 4 à 8 décimales
			timestamp,
		}

		if err := writer.Write(record); err != nil {
			return "", err
		}
	}

	return filename, nil
}

// ExportPairToCSV exporte les données d'une paire vers un fichier CSV
func ExportPairToCSV(db *sql.DB, pair string) (string, error) {
	csvDir := initCSVDirectory()
	filename := fmt.Sprintf("%s_%s", pair, generateCSVFilename())
	filePath := filepath.Join(csvDir, filename)

	// Récupérer les données de la paire
	rows, err := db.Query(
		"SELECT pair, ask_price, bid_price, last_trade_price, volume, high_price, low_price, timestamp FROM crypto_data WHERE pair = ?",
		pair,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	// Créer le fichier CSV
	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Écrire l'en-tête
	headers := []string{"Pair", "Ask", "Bid", "Last", "Volume", "High", "Low", "Timestamp"}
	if err := writer.Write(headers); err != nil {
		return "", err
	}

	// Écrire les données
	for rows.Next() {
		var pair string
		var ask, bid, lastTrade, volume, high, low float64
		var timestamp string
		if err := rows.Scan(&pair, &ask, &bid, &lastTrade, &volume, &high, &low, &timestamp); err != nil {
			return "", err
		}

		record := []string{
			pair,
			strconv.FormatFloat(ask, 'f', 8, 64),       // Augmenté à 8 décimales
			strconv.FormatFloat(bid, 'f', 8, 64),       // Augmenté à 8 décimales
			strconv.FormatFloat(lastTrade, 'f', 8, 64), // Augmenté à 8 décimales
			strconv.FormatFloat(volume, 'f', 4, 64),    // Volume reste à 4 décimales
			strconv.FormatFloat(high, 'f', 8, 64),      // Augmenté à 8 décimales
			strconv.FormatFloat(low, 'f', 8, 64),       // Augmenté à 8 décimales
			timestamp,
		}

		if err := writer.Write(record); err != nil {
			return "", err
		}
	}

	return filename, nil
}

// ------------------- Partie Serveur Web -------------------

// Gestionnaire pour la route principale
func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Crypto Archive API\n")
	fmt.Fprintf(w, "Routes disponibles:\n")
	fmt.Fprintf(w, "- GET /api/status : Statut du serveur\n")
	fmt.Fprintf(w, "- GET /api/pairs : Liste des paires disponibles\n")
	fmt.Fprintf(w, "- GET /api/data/<pair> : Données pour une paire spécifique\n")
	fmt.Fprintf(w, "- GET /api/export/<pair> : Télécharger CSV pour une paire\n")
	fmt.Fprintf(w, "- GET /api/export-latest : Télécharger le dernier fichier CSV global\n")
}

// Gestionnaire pour le statut du serveur
func statusHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverTime, err := GetServerStatus()
		if err != nil {
			http.Error(w, "Erreur lors de la récupération du statut", http.StatusInternalServerError)
			return
		}

		status := struct {
			ServerTime    int64  `json:"server_time"`
			ServerTimeRFC string `json:"server_time_rfc"`
			LocalTime     int64  `json:"local_time"`
			TimeDiff      int64  `json:"time_diff"`
			DatabaseOK    bool   `json:"database_ok"`
		}{
			ServerTime:    serverTime.Unixtime,
			ServerTimeRFC: serverTime.RFC1123,
			LocalTime:     time.Now().Unix(),
			TimeDiff:      time.Now().Unix() - serverTime.Unixtime,
			DatabaseOK:    db.Ping() == nil,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}

// Gestionnaire pour la liste des paires
func pairsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT DISTINCT pair FROM crypto_data")
		if err != nil {
			http.Error(w, "Erreur lors de la récupération des paires", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var pairs []string
		for rows.Next() {
			var pair string
			if err := rows.Scan(&pair); err != nil {
				http.Error(w, "Erreur lors de la lecture des paires", http.StatusInternalServerError)
				return
			}
			pairs = append(pairs, pair)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pairs)
	}
}

// Gestionnaire pour les données d'une paire
func pairDataHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extraire le nom de la paire de l'URL
		path := r.URL.Path
		// Normaliser le chemin pour gérer le cas avec ou sans slash à la fin
		if path == "/api/data" || path == "/api/data/" {
			// Si aucune paire spécifique n'est demandée, retourner toutes les paires
			rows, err := db.Query(
				"SELECT pair, ask_price, bid_price, last_trade_price, volume, high_price, low_price, timestamp FROM crypto_data",
			)
			if err != nil {
				http.Error(w, "Erreur lors de la récupération des données", http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			var allData []map[string]interface{}
			for rows.Next() {
				var pair string
				var ask, bid, lastTrade, volume, high, low float64
				var timestamp string
				if err := rows.Scan(&pair, &ask, &bid, &lastTrade, &volume, &high, &low, &timestamp); err != nil {
					http.Error(w, "Erreur lors de la lecture des données", http.StatusInternalServerError)
					return
				}

				entry := map[string]interface{}{
					"pair":      pair,
					"ask":       ask,
					"bid":       bid,
					"last":      lastTrade,
					"volume":    volume,
					"high":      high,
					"low":       low,
					"timestamp": timestamp,
				}
				allData = append(allData, entry)
			}

			if len(allData) == 0 {
				http.Error(w, "Aucune donnée disponible", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(allData)
			return
		}

		// Extraire le nom de la paire
		pair := path[len("/api/data/"):]
		if pair == "" {
			http.Error(w, "Paire non spécifiée", http.StatusBadRequest)
			return
		}

		rows, err := db.Query(
			"SELECT pair, ask_price, bid_price, last_trade_price, volume, high_price, low_price, timestamp FROM crypto_data WHERE pair = ?",
			pair,
		)
		if err != nil {
			http.Error(w, "Erreur lors de la récupération des données", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var data []map[string]interface{}
		for rows.Next() {
			var pair string
			var ask, bid, lastTrade, volume, high, low float64
			var timestamp string
			if err := rows.Scan(&pair, &ask, &bid, &lastTrade, &volume, &high, &low, &timestamp); err != nil {
				http.Error(w, "Erreur lors de la lecture des données", http.StatusInternalServerError)
				return
			}

			entry := map[string]interface{}{
				"pair":      pair,
				"ask":       ask,
				"bid":       bid,
				"last":      lastTrade,
				"volume":    volume,
				"high":      high,
				"low":       low,
				"timestamp": timestamp,
			}
			data = append(data, entry)
		}

		if len(data) == 0 {
			http.Error(w, "Paire non trouvée", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

// Gestionnaire pour télécharger un fichier CSV
func exportCSVHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pair := r.URL.Path[len("/api/export/"):]
		if pair == "" {
			http.Error(w, "Paire non spécifiée", http.StatusBadRequest)
			return
		}

		filename, err := ExportPairToCSV(db, pair)
		if err != nil {
			http.Error(w, "Erreur lors de l'export CSV", http.StatusInternalServerError)
			return
		}

		csvDir := initCSVDirectory()
		filePath := filepath.Join(csvDir, filename)

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		w.Header().Set("Content-Type", "text/csv")
		http.ServeFile(w, r, filePath)
	}
}

// Gestionnaire pour télécharger le dernier fichier CSV généré
func exportLatestCSVHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Trouver le fichier le plus récent dans le répertoire CSV
		csvDir := initCSVDirectory()
		files, err := os.ReadDir(csvDir)
		if err != nil {
			http.Error(w, "Erreur lors de la lecture du répertoire CSV", http.StatusInternalServerError)
			return
		}

		if len(files) == 0 {
			// Si pas de fichier, en générer un nouveau
			filename, err := ExportAllPairsToSingleCSV(db)
			if err != nil {
				http.Error(w, "Erreur lors de l'export CSV", http.StatusInternalServerError)
				return
			}

			filePath := filepath.Join(csvDir, filename)
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
			w.Header().Set("Content-Type", "text/csv")
			http.ServeFile(w, r, filePath)
			return
		}

		// Trouver le fichier le plus récent
		var latestFile os.DirEntry
		var latestTime time.Time

		for _, file := range files {
			if !file.IsDir() && strings.HasPrefix(file.Name(), "crypto_data_") {
				info, err := file.Info()
				if err != nil {
					continue
				}

				if latestFile == nil || info.ModTime().After(latestTime) {
					latestFile = file
					latestTime = info.ModTime()
				}
			}
		}

		if latestFile == nil {
			http.Error(w, "Aucun fichier CSV disponible", http.StatusNotFound)
			return
		}

		filePath := filepath.Join(csvDir, latestFile.Name())
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", latestFile.Name()))
		w.Header().Set("Content-Type", "text/csv")
		http.ServeFile(w, r, filePath)
	}
}

// Configurer le serveur HTTP
func setupHTTPServer(db *sql.DB) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/api/status", statusHandler(db))
	mux.HandleFunc("/api/pairs", pairsHandler(db))
	mux.HandleFunc("/api/data/", pairDataHandler(db))
	mux.HandleFunc("/api/export/", exportCSVHandler(db))
	mux.HandleFunc("/api/export-latest", exportLatestCSVHandler(db))

	// Servir les fichiers CSV statiques
	csvDir := initCSVDirectory()
	fs := http.FileServer(http.Dir(csvDir))
	mux.Handle("/csv/", http.StripPrefix("/csv/", fs))

	return &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
}

// ------------------- Archivage des données -------------------

// ArchiveData récupère les données de toutes les Asset Pairs et les stocke dans la BDD.
func ArchiveData(db *sql.DB) {
	pairs, err := GetAssetPairs()
	if err != nil {
		log.Println("Erreur récupération des paires:", err)
		return
	}
	log.Printf("Nombre de paires récupérées: %d\n", len(pairs))

	for _, pair := range pairs {
		// Utiliser le nom interne pour la requête Ticker
		tickerInfo, err := GetTicker(pair.InternalName)
		if err != nil {
			log.Println("Erreur récupération ticker pour", pair.InternalName, ":", err)
			continue
		}

		ask, _ := strconv.ParseFloat(tickerInfo.Ask[0], 64)
		bid, _ := strconv.ParseFloat(tickerInfo.Bid[0], 64)
		lastTrade, _ := strconv.ParseFloat(tickerInfo.Last[0], 64)
		volume, _ := strconv.ParseFloat(tickerInfo.Volume[1], 64)
		high, _ := strconv.ParseFloat(tickerInfo.High[0], 64)
		low, _ := strconv.ParseFloat(tickerInfo.Low[0], 64)
		timestamp := time.Now().Format(time.RFC3339)

		// Stocker avec le nom alternatif pour l'affichage
		InsertCryptoData(db, pair.AltName, ask, bid, lastTrade, volume, high, low, timestamp)
		log.Printf("Archivé : %s | Ask: %.8f | Bid: %.8f | Last: %.8f | High: %.8f | Low: %.8f\n",
			pair.AltName, ask, bid, lastTrade, high, low) // Augmenté de 4 à 8 décimales et ajouté High/Low
	}
}

// ArchiveDataContinuously lance l'archivage des données à intervalles réguliers
func ArchiveDataContinuously(db *sql.DB, interval time.Duration, stopChan <-chan struct{}, wg *sync.WaitGroup) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer wg.Done()

	counter := 0 // Compteur pour l'export CSV

	for {
		select {
		case <-ticker.C:
			log.Println("Démarrage d'un cycle d'archivage...")
			ArchiveData(db)

			counter++
			log.Printf("Cycle d'archivage %d/5 terminé. Prochain export CSV dans %d minutes.", counter, 5-counter)

			// Export CSV toutes les 5 minutes (après 5 cycles d'archivage d'une minute)
			if counter >= 5 {
				log.Println("Export du fichier CSV global...")
				filename, err := ExportAllPairsToSingleCSV(db)
				if err != nil {
					log.Printf("Erreur lors de l'export CSV: %v", err)
				} else {
					log.Printf("Fichier CSV exporté: %s", filename)
					counter = 0 // Réinitialiser le compteur
				}
			}

		case <-stopChan:
			log.Println("Archivage arrêté")
			return
		}
	}
}

// ------------------- Affichage en Terminal -------------------

// DisplayArchivedData affiche le contenu de la base SQLite dans le terminal.
func DisplayArchivedData(db *sql.DB) {
	rows, err := db.Query("SELECT pair, ask_price, bid_price, last_trade_price, volume, high_price, low_price, timestamp FROM crypto_data")
	if err != nil {
		log.Println("Erreur lors de la lecture de la BDD:", err)
		return
	}
	defer rows.Close()

	fmt.Println("----- Données archivées -----")
	for rows.Next() {
		var pair string
		var ask, bid, lastTrade, volume, high, low float64
		var timestamp string
		if err := rows.Scan(&pair, &ask, &bid, &lastTrade, &volume, &high, &low, &timestamp); err != nil {
			log.Println("Erreur de lecture:", err)
			continue
		}
		fmt.Printf("Paire: %s | Ask: %.8f | Bid: %.8f | Last: %.8f | Vol: %.2f | High: %.8f | Low: %.8f | Timestamp: %s\n", 
			pair, ask, bid, lastTrade, volume, high, low, timestamp) // Augmenté à 8 décimales pour les prix
	}
	fmt.Println("-------------------------------")
}

// ------------------- Fonction principale -------------------

func main() {
	// Créer le dossier "data" s'il n'existe pas pour la base SQLite
	if _, err := os.Stat("data"); os.IsNotExist(err) {
		os.Mkdir("data", 0755)
	}

	// Initialiser la base SQLite dans "data/crypto.db"
	db := InitDB("data/crypto.db")
	defer db.Close()

	// Nettoyer la base pour repartir sur des données réelles
	CleanDB(db)

	// Vérifier le statut du serveur Kraken
	serverTime, err := GetServerStatus()
	if err != nil {
		log.Printf("Erreur lors de la récupération du statut du serveur: %v", err)
	} else {
		log.Printf("Statut du serveur Kraken: \n")
		log.Printf("- Timestamp Unix: %d\n", serverTime.Unixtime)
		log.Printf("- Heure RFC1123: %s\n", serverTime.RFC1123)
		log.Printf("- Décalage avec serveur: %d secondes\n",
			time.Now().Unix()-serverTime.Unixtime)
	}

	// Mettre en place le serveur HTTP
	server := setupHTTPServer(db)

	// Lancer le serveur HTTP dans une goroutine
	go func() {
		log.Printf("Serveur HTTP démarré sur %s\n", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Erreur serveur HTTP: %v", err)
		}
	}()

	// Configurer l'archivage des données
	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// Archivage toutes les minutes pour mise à jour fréquente
	// et export CSV toutes les 5 minutes
	wg.Add(1)
	go ArchiveDataContinuously(db, 1*time.Minute, stopChan, &wg)

	// Attendre l'arrêt (Ctrl+C)
	fmt.Println("Serveur démarré. Appuyez sur Ctrl+C pour arrêter.")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	// Arrêt propre
	log.Println("Arrêt du serveur...")
	close(stopChan)
	wg.Wait()
	log.Println("Serveur arrêté")
}
