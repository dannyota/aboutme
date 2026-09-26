output "zone_id" {
  value = aws_route53_zone.zone.zone_id
}

output "name_servers" {
  value = aws_route53_zone.zone.name_servers
}

# The values the owner enters at the .vn registrar (docs/runbooks/dns.md).
# Registrars ask for either the DS fields or the DNSKEY fields.
output "dnssec" {
  value = {
    key_tag     = aws_route53_key_signing_key.ksk.key_tag
    algorithm   = aws_route53_key_signing_key.ksk.signing_algorithm_type
    digest_type = aws_route53_key_signing_key.ksk.digest_algorithm_type
    digest      = aws_route53_key_signing_key.ksk.digest_value
    ds_record   = aws_route53_key_signing_key.ksk.ds_record
    flags       = aws_route53_key_signing_key.ksk.flag
    public_key  = aws_route53_key_signing_key.ksk.public_key
  }
}
