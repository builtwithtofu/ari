env "globaldb" {
  # This URL is reference-only for local Atlas tooling.
  # Runtime bootstrap passes --url from daemon config.
  url = "sqlite://${HOME}/.ari/ari.db"

  migration {
    dir = "file://migrations"
  }
}
