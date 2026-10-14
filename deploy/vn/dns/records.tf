resource "cloudflare_dns_record" "address" {
  for_each = { apex = "aboutme.vn", www = "www.aboutme.vn" }
  zone_id  = var.zone_id
  name     = each.value
  type     = "A"
  content  = var.host_ipv4
  ttl      = 300
  proxied  = false

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_dns_record" "caa" {
  zone_id = var.zone_id
  name    = "aboutme.vn"
  proxied = false
  type    = "CAA"
  ttl     = var.record_ttls.caa
  data = {
    flags = 0
    tag   = "issue"
    value = "letsencrypt.org"
  }
}

resource "cloudflare_dns_record" "mx" {
  zone_id  = var.zone_id
  name     = "aboutme.vn"
  proxied  = false
  type     = "MX"
  content  = "smtp.google.com"
  priority = var.mx_priority
  ttl      = var.record_ttls.mx

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_dns_record" "txt" {
  for_each = {
    spf         = "aboutme.vn"
    dmarc       = "_dmarc.aboutme.vn"
    dkim        = "dkim._domainkey.aboutme.vn"
    etrans      = "etrans._domainkey.aboutme.vn"
    google_dkim = "google._domainkey.aboutme.vn"
  }
  zone_id = var.zone_id
  name    = each.value
  proxied = false
  type    = "TXT"
  ttl     = var.record_ttls[each.key]
  content = each.key == "spf" ? "\"v=spf1 include:_spf.bizflycloud.vn include:_spf.google.com ~all\"" : each.key == "dmarc" ? "\"v=DMARC1; p=none; rua=mailto:danny@aboutme.vn\"" : var.dkim_values[each.key]
}

resource "cloudflare_dns_record" "google_verification" {
  zone_id = var.zone_id
  name    = var.google_verification_name
  type    = "CNAME"
  content = var.google_verification_target
  ttl     = var.record_ttls.google_verification
  proxied = false
}
