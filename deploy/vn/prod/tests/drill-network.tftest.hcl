mock_provider "vngcloud" {}

variables {
  project_id           = "pro-test"
  admin_ssh_public_key = "ssh-ed25519 AAAATEST aboutme-test"
  state_passphrase     = "test-only-passphrase"
}

run "drill_network_disabled_by_default" {
  command = plan

  assert {
    condition     = length(vngcloud_vserver_secgroup.drill) == 0
    error_message = "The default configuration must not create drill security groups."
  }

  assert {
    condition     = length(vngcloud_vserver_secgrouprule.drill) == 0
    error_message = "The default configuration must not create drill security-group rules."
  }

  assert {
    condition     = output.drill_security_group_ids == null && output.drill_security_group_rules == null
    error_message = "Disabled drill-network outputs must be null."
  }
}

run "drill_network_has_exact_permits" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32", "8.8.8.8/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24", "9.9.9.0/24"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  assert {
    condition = toset(keys(vngcloud_vserver_secgroup.drill)) == toset([
      "bootstrap",
      "restore",
    ])
    error_message = "An enabled drill network must create only bootstrap and restore groups."
  }

  assert {
    condition     = vngcloud_vserver_secgroup.drill["bootstrap"].name == "aboutme-drill-bootstrap" && vngcloud_vserver_secgroup.drill["restore"].name == "aboutme-drill-restore"
    error_message = "Drill security-group names must remain stable for reconciliation."
  }

  assert {
    condition = output.drill_security_group_ids == {
      bootstrap = vngcloud_vserver_secgroup.drill["bootstrap"].id
      restore   = vngcloud_vserver_secgroup.drill["restore"].id
    }
    error_message = "The drill group output must expose both provider IDs."
  }

  assert {
    condition = toset(output.drill_security_group_rules) == toset([
      { group = "bootstrap", direction = "ingress", ethertype = "IPv4", protocol = "TCP", port_min = 22922, port_max = 22922, remote_ip_prefix = "10.76.1.10/32" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "TCP", port_min = 53, port_max = 53, remote_ip_prefix = "1.1.1.1/32" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "UDP", port_min = 53, port_max = 53, remote_ip_prefix = "1.1.1.1/32" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "TCP", port_min = 53, port_max = 53, remote_ip_prefix = "8.8.8.8/32" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "UDP", port_min = 53, port_max = 53, remote_ip_prefix = "8.8.8.8/32" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "UDP", port_min = 123, port_max = 123, remote_ip_prefix = "129.6.15.28/32" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "TCP", port_min = 80, port_max = 80, remote_ip_prefix = "8.8.4.0/24" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "TCP", port_min = 443, port_max = 443, remote_ip_prefix = "8.8.4.0/24" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "TCP", port_min = 80, port_max = 80, remote_ip_prefix = "9.9.9.0/24" },
      { group = "bootstrap", direction = "egress", ethertype = "IPv4", protocol = "TCP", port_min = 443, port_max = 443, remote_ip_prefix = "9.9.9.0/24" },
      { group = "restore", direction = "ingress", ethertype = "IPv4", protocol = "TCP", port_min = 22922, port_max = 22922, remote_ip_prefix = "10.76.1.10/32" },
      { group = "restore", direction = "egress", ethertype = "IPv4", protocol = "TCP", port_min = 443, port_max = 443, remote_ip_prefix = "1.0.0.0/24" },
    ])
    error_message = "The drill groups must expose exactly the reviewed rule tuples."
  }

  assert {
    condition = length(vngcloud_vserver_secgrouprule.drill) == length(local.drill_rules) && alltrue([
      for key, expected in local.drill_rules :
      vngcloud_vserver_secgrouprule.drill[key].direction == expected.direction &&
      vngcloud_vserver_secgrouprule.drill[key].ethertype == expected.ethertype &&
      vngcloud_vserver_secgrouprule.drill[key].protocol == expected.protocol &&
      vngcloud_vserver_secgrouprule.drill[key].port_range_min == expected.port_min &&
      vngcloud_vserver_secgrouprule.drill[key].port_range_max == expected.port_max &&
      vngcloud_vserver_secgrouprule.drill[key].remote_ip_prefix == expected.remote_ip_prefix &&
      vngcloud_vserver_secgrouprule.drill[key].security_group_id == vngcloud_vserver_secgroup.drill[expected.group].id
    ])
    error_message = "Each provider rule must match its exported tuple and target group."
  }

  assert {
    condition     = length(vngcloud_vserver_server.main.security_group) == 1 && vngcloud_vserver_server.main.security_group[0] == vngcloud_vserver_secgroup.main.id
    error_message = "The production server must remain attached only to the production group."
  }
}

run "rejects_empty_destination_sets" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = []
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_null_destination_set" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = null
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_malformed_production_address" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "not-a-cidr"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_ipv6_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["2001:4860:4860::8888/128"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_unrestricted_package_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["0.0.0.0/0"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_private_storage_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["172.16.0.0/12"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_metadata_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["169.254.169.254/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_production_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["10.76.1.10/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_malformed_package_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["not-a-cidr"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_loopback_package_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["127.0.0.0/8"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_link_local_package_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["169.254.0.0/16"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_multicast_storage_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["224.0.0.0/4"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_shared_address_space" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["100.64.0.0/10"]
      hcm04_cidrs             = ["1.0.0.0/24"]
    }
  }

  expect_failures = [var.drill_network]
}

run "rejects_reserved_storage_destination" {
  command = plan

  variables {
    drill_network = {
      production_private_cidr = "10.76.1.10/32"
      resolver_cidrs          = ["1.1.1.1/32"]
      time_service_cidrs      = ["129.6.15.28/32"]
      package_cidrs           = ["8.8.4.0/24"]
      hcm04_cidrs             = ["240.0.0.0/4"]
    }
  }

  expect_failures = [var.drill_network]
}
