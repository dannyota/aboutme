mock_provider "cloudflare" {}

variables {
  zone_id                    = "00000000000000000000000000000001"
  host_ipv4                  = "192.0.2.10"
  dkim_values                = { dkim = "test-bizfly-verification", etrans = "test-bizfly-signing", google_dkim = "test-google-signing" }
  google_verification_name   = "verification.aboutme.vn"
  google_verification_target = "verification.example.com"
  record_ttls                = { caa = 1, mx = 3600, spf = 300, dmarc = 600, dkim = 900, etrans = 1200, google_dkim = 1800, google_verification = 7200 }
  mx_priority                = 1
  state_passphrase           = "test-only-passphrase"
}

run "records_match_inventory" {
  command = plan

  assert {
    condition = length(cloudflare_dns_record.address) == 2 && alltrue([
      for key, name in { apex = "aboutme.vn", www = "www.aboutme.vn" } :
      cloudflare_dns_record.address[key].name == name &&
      cloudflare_dns_record.address[key].type == "A" &&
      cloudflare_dns_record.address[key].ttl == 300 &&
      cloudflare_dns_record.address[key].proxied == false &&
      cloudflare_dns_record.address[key].content == "192.0.2.10"
    ])
    error_message = "Both address records must serve the host directly with TTL 300."
  }

  assert {
    condition     = cloudflare_dns_record.caa.name == "aboutme.vn" && cloudflare_dns_record.caa.type == "CAA" && cloudflare_dns_record.caa.proxied == false && cloudflare_dns_record.caa.ttl == var.record_ttls.caa && cloudflare_dns_record.caa.data.flags == 0 && cloudflare_dns_record.caa.data.tag == "issue" && cloudflare_dns_record.caa.data.value == "letsencrypt.org"
    error_message = "The managed CAA record must allow Let's Encrypt."
  }

  assert {
    condition     = cloudflare_dns_record.mx.name == "aboutme.vn" && cloudflare_dns_record.mx.type == "MX" && cloudflare_dns_record.mx.proxied == false && cloudflare_dns_record.mx.ttl == var.record_ttls.mx && cloudflare_dns_record.mx.content == "smtp.google.com" && cloudflare_dns_record.mx.priority == 1
    error_message = "The MX record must preserve the inventoried TTL and priority."
  }

  assert {
    condition = length(cloudflare_dns_record.txt) == 5 && alltrue([
      for key, expected in {
        spf         = { name = "aboutme.vn", content = "\"v=spf1 include:_spf.bizflycloud.vn include:_spf.google.com ~all\"" }
        dmarc       = { name = "_dmarc.aboutme.vn", content = "\"v=DMARC1; p=none; rua=mailto:danny@aboutme.vn\"" }
        dkim        = { name = "dkim._domainkey.aboutme.vn", content = "test-bizfly-verification" }
        etrans      = { name = "etrans._domainkey.aboutme.vn", content = "test-bizfly-signing" }
        google_dkim = { name = "google._domainkey.aboutme.vn", content = "test-google-signing" }
      } : cloudflare_dns_record.txt[key].name == expected.name && cloudflare_dns_record.txt[key].type == "TXT" && cloudflare_dns_record.txt[key].proxied == false && cloudflare_dns_record.txt[key].ttl == var.record_ttls[key] && cloudflare_dns_record.txt[key].content == expected.content
    ])
    error_message = "The five TXT records must match the mail inventory."
  }

  assert {
    condition     = cloudflare_dns_record.google_verification.name == "verification.aboutme.vn" && cloudflare_dns_record.google_verification.type == "CNAME" && cloudflare_dns_record.google_verification.ttl == var.record_ttls.google_verification && cloudflare_dns_record.google_verification.proxied == false && cloudflare_dns_record.google_verification.content == "verification.example.com"
    error_message = "Google verification must remain a DNS-only CNAME."
  }
}

run "rejects_empty_ipv4" {
  command = plan
  variables {
    host_ipv4 = ""
  }
  expect_failures = [var.host_ipv4]
}

run "rejects_malformed_ipv4" {
  command = plan
  variables {
    host_ipv4 = "not-an-ip"
  }
  expect_failures = [var.host_ipv4]
}

run "rejects_out_of_range_ipv4" {
  command = plan
  variables {
    host_ipv4 = "256.1.2.3"
  }
  expect_failures = [var.host_ipv4]
}

run "rejects_ipv6" {
  command = plan
  variables {
    host_ipv4 = "2001:db8::1"
  }
  expect_failures = [var.host_ipv4]
}

run "rejects_cidr" {
  command = plan
  variables {
    host_ipv4 = "192.0.2.10/32"
  }
  expect_failures = [var.host_ipv4]
}

run "rejects_empty_dkim" {
  command = plan
  variables {
    dkim_values = { dkim = " ", etrans = "test-key", google_dkim = "test-key" }
  }
  expect_failures = [var.dkim_values]
}

run "rejects_empty_etrans" {
  command = plan
  variables {
    dkim_values = { dkim = "test-key", etrans = " ", google_dkim = "test-key" }
  }
  expect_failures = [var.dkim_values]
}

run "rejects_empty_google_dkim" {
  command = plan
  variables {
    dkim_values = { dkim = "test-key", etrans = "test-key", google_dkim = " " }
  }
  expect_failures = [var.dkim_values]
}

run "rejects_invalid_zone_id" {
  command = plan
  variables {
    zone_id = ""
  }
  expect_failures = [var.zone_id]
}

run "rejects_invalid_google_verification_name" {
  command = plan
  variables {
    google_verification_name = ""
  }
  expect_failures = [var.google_verification_name]
}

run "rejects_invalid_google_verification_target" {
  command = plan
  variables {
    google_verification_target = "bad target"
  }
  expect_failures = [var.google_verification_target]
}

run "rejects_invalid_record_ids" {
  command = plan
  variables {
    record_ids = { unknown = "00000000000000000000000000000001" }
  }
  expect_failures = [var.record_ids]
}

run "rejects_invalid_record_ttls" {
  command = plan
  variables {
    record_ttls = { caa = 0, mx = 1, spf = 1, dmarc = 1, dkim = 1, etrans = 1, google_dkim = 1, google_verification = 1 }
  }
  expect_failures = [var.record_ttls]
}

run "rejects_invalid_mx_priority" {
  command = plan
  variables {
    mx_priority = -1
  }
  expect_failures = [var.mx_priority]
}

run "rejects_empty_dkim_values" {
  command = plan
  variables {
    dkim_values = { dkim = "", etrans = "", google_dkim = "" }
  }
  expect_failures = [var.dkim_values]
}

run "rejects_malformed_record_id" {
  command = plan
  variables {
    record_ids = { apex = "not-a-record-id" }
  }
  expect_failures = [var.record_ids]
}

run "rejects_verification_name_outside_zone" {
  command = plan
  variables {
    google_verification_name = "verification.example.com"
  }
  expect_failures = [var.google_verification_name]
}
