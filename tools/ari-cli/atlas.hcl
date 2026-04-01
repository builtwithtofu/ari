env "globaldb" {
  url = "sqlite://${HOME}/.ari/ari.db"

  migration {
    dir = "file://migrations"
  }
}
