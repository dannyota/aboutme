# Remove this file after all ten records are imported and the next plan is empty.
import {
  for_each = { for key, id in var.record_ids : key => id if contains(["apex", "www"], key) }
  to       = cloudflare_dns_record.address[each.key]
  id       = "${var.zone_id}/${each.value}"
}

import {
  for_each = { for key, id in var.record_ids : key => id if contains(["spf", "dmarc", "dkim", "etrans", "google_dkim"], key) }
  to       = cloudflare_dns_record.txt[each.key]
  id       = "${var.zone_id}/${each.value}"
}

import {
  for_each = { for key, id in var.record_ids : key => id if key == "caa" }
  to       = cloudflare_dns_record.caa
  id       = "${var.zone_id}/${each.value}"
}

import {
  for_each = { for key, id in var.record_ids : key => id if key == "mx" }
  to       = cloudflare_dns_record.mx
  id       = "${var.zone_id}/${each.value}"
}

import {
  for_each = { for key, id in var.record_ids : key => id if key == "google_verification" }
  to       = cloudflare_dns_record.google_verification
  id       = "${var.zone_id}/${each.value}"
}
