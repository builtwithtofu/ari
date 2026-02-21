env "globaldb" {
  url = "sqlite://${HOME}/.config/ari/ari.db"

  migration {
    dir = "file://migrations"
  }
}
