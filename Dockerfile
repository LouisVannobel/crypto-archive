# Étape 1 : Builder l'application avec une image Go officielle
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copier les fichiers de dépendances et les télécharger
COPY go.mod go.sum* ./
RUN go mod download && go mod verify

# Copier le code source
COPY . .

# Compiler l'application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o crypto-archive .

# Étape 2 : Image finale
FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite

WORKDIR /app

# Créer les répertoires nécessaires
RUN mkdir -p /app/data/csv

# Copier l'exécutable compilé depuis l'étape de construction
COPY --from=builder /app/crypto-archive /app/

# Exposer le port du serveur web
EXPOSE 8080

# Définir le point d'entrée
CMD ["/app/crypto-archive"]
