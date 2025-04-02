# Crypto Archive API

Une application Go qui archive les données de crypto-monnaies depuis l'API Kraken et les met à disposition via une API REST.

## Table des matières

- [À propos](#à-propos)
- [Fonctionnalités](#fonctionnalités)
- [Technologies utilisées](#technologies-utilisées)
- [Installation](#installation)
  - [Avec Docker](#avec-docker)
  - [Sans Docker](#sans-docker)
- [Utilisation](#utilisation)
  - [Routes API](#routes-api)
  - [Structure des fichiers CSV](#structure-des-fichiers-csv)
- [Contribuer](#contribuer)
- [Licence](#licence)

---

## À propos

Crypto Archive API est une application écrite en Go qui permet de collecter, archiver et exposer les données de trading des crypto-monnaies via une API REST. Elle utilise l'API Kraken pour récupérer les données en temps réel et les stocke dans une base de données SQLite. Les utilisateurs peuvent également exporter les données sous forme de fichiers CSV.

![terminal1](https://github.com/user-attachments/assets/b948aadb-728f-4f86-a9bd-0632bd6737a9)
![terminal2](https://github.com/user-attachments/assets/6bc64b1c-abe8-4ac0-994b-2df5d9b4313e)

---

## Fonctionnalités

- **Récupération des données Kraken** :
  - Statut et timing du serveur
  - Liste des paires de trading
  - Informations détaillées sur chaque paire (ask, bid, last, volume, high, low)
- **Archivage des données** :
  - Stockage dans une base SQLite
  - Mise à jour automatique toutes les minutes
- **Export CSV** :
  - Génération automatique de fichiers CSV toutes les 5 minutes
  - Téléchargement des fichiers via l'API
- **API REST** :
  - Accès aux données archivées
  - Téléchargement des fichiers CSV

---

## Technologies utilisées

- **Langage** : Go (Golang)
- **Base de données** : SQLite
- **Serveur HTTP** : Go net/http
- **Conteneurisation** : Docker
- **Gestion des tâches asynchrones** : Goroutines, WaitGroups, Channels

---

## Installation

### Avec Docker

1. **Cloner le repository** :
   ```bash
   git clone https://github.com/LouisVannobel/crypto-archive.git
   cd crypto-archive
   ```

2. **Lancer l'application avec Docker Compose** :
   ```bash
   docker-compose up -d
   ```

3. **Accéder à l'API** :
   L'application sera disponible sur `http://localhost:8080`.

### Sans Docker

1. **Cloner le repository** :
   ```bash
   git clone https://github.com/LouisVannobel/crypto-archive.git
   cd crypto-archive
   ```

2. **Installer les dépendances** :
   ```bash
   go mod download
   ```

3. **Compiler et lancer l'application** :
   ```bash
   go build -o crypto-archive
   ./crypto-archive
   ```

4. **Accéder à l'API** :
   L'application sera disponible sur `http://localhost:8080`.

---

## Utilisation

### Routes API

- `GET /` : Documentation de l'API
- ![home](https://github.com/user-attachments/assets/7122c7fb-0802-4030-bd3b-d6e29ef4e99e)

- `GET /api/status` : Statut du serveur (temps du serveur Kraken, état de la base de données, etc.)
- ![api-status](https://github.com/user-attachments/assets/016f71ec-f8fb-4f92-a87e-4b57d50622d4)

- `GET /api/pairs` : Liste des paires disponibles
- ![api-pairs](https://github.com/user-attachments/assets/cb45b69d-1eb2-43db-b732-541552cede1d)

- `GET /api/data` : Données archivées pour toutes les paires
- ![api-data](https://github.com/user-attachments/assets/da858145-5e48-4350-8328-a9fecd0857ca)

- `GET /api/data/<pair>` : Données archivées pour une paire spécifique
- `GET /api/export/<pair>` : Télécharger un fichier CSV pour une paire spécifique
- `GET /api/export-latest` : Télécharger le dernier fichier CSV global

### Structure des fichiers CSV

Les fichiers CSV sont générés toutes les 5 minutes avec le format suivant :
```
crypto_data_day_month_year_hour_minutes.csv
```

Exemple : `crypto_data_01_01_2025_12_05.csv`

Chaque fichier contient les colonnes suivantes :
- **Pair** : Nom de la paire (ex: BTCUSD)
- **Ask** : Prix de vente
- **Bid** : Prix d'achat
- **Last** : Dernier prix échangé
- **Volume** : Volume échangé sur 24h
- **High** : Prix le plus haut sur 24h
- **Low** : Prix le plus bas sur 24h
- **Timestamp** : Date et heure de l'enregistrement

---

## Licence

Ce projet est sous licence **GNU Affero General Public License v3.0**. Consultez le fichier [LICENSE](./LICENSE) pour plus d'informations.
