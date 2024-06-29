# Primera etapa: compilar la aplicación
FROM golang:latest AS build

# Establece el directorio de trabajo dentro del contenedor
WORKDIR /app

# Copia solo los archivos necesarios para la compilación
COPY go.mod go.sum ./
RUN go mod download

# Copia el resto del código fuente
COPY . .

# Compila la aplicación
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Segunda etapa para la imagen final
FROM alpine:latest

# Instala ffmpeg en la imagen Alpine
RUN apk --no-cache add ffmpeg

# Establece el directorio de trabajo dentro del contenedor
WORKDIR /app

# Copia solo los archivos necesarios desde la etapa anterior
COPY --from=build /app/main .

# Expone el puerto en el que la aplicación se ejecutará
EXPOSE 8080

# Comando para ejecutar la aplicación
CMD ["./main"]