#!/usr/bin/env python3
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location("oci_rules", pathlib.Path(__file__).parents[1] / "scripts" / "oci-nsg.py")
rules = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rules)

class CloudRules(unittest.TestCase):
    def test_unrelated_security_list_rule_is_preserved(self):
        ssh = {"protocol":"6", "source":"192.0.2.0/24", "source-type":"CIDR_BLOCK", "is-stateless":False, "tcp-options":{"destination-port-range":{"min":22,"max":22}}}
        obsolete = {"protocol":"6", "source":"0.0.0.0/0", "description":rules.MARKER + "old", "tcp-options":{"destination-port-range":{"min":8888,"max":8888}}}
        new = {"protocol":"17", "source":"0.0.0.0/0", "sourceType":"CIDR_BLOCK", "isStateless":False, "udpOptions":{"destinationPortRange":{"min":7777,"max":7777}}, "description":rules.MARKER + "17/7777"}
        calls = []
        def fake(*args):
            calls.append(args)
            if "get" in args:
                return {"data":{"ingress-security-rules":[ssh,obsolete]},"etag":"original-version"}
            return {}
        rules.run = fake
        rules.sync_target({"kind":"security-list","id":"ocid1.securitylist.test"},[new])
        update = calls[-1]
        import json
        payload = json.loads(update[update.index("--ingress-security-rules")+1])
        self.assertIn(rules.camel(ssh),payload)
        self.assertIn(new,payload)
        self.assertNotIn(rules.camel(obsolete),payload)
        self.assertEqual(update[update.index("--if-match")+1],"original-version")

    def test_missing_etag_never_overwrites_rules(self):
        rules.run = lambda *args: {"data":{"ingress-security-rules":[{"description":rules.MARKER+"old"}]}}
        with self.assertRaises(RuntimeError):
            rules.sync_target({"kind":"security-list","id":"ocid1.securitylist.test"},[])

    def test_unchanged_nsg_is_idempotent(self):
        current = {"id":"rule-id", "direction":"INGRESS", "protocol":"17", "source":"0.0.0.0/0", "is-stateless":False, "udp-options":{"destination-port-range":{"min":7777,"max":7777}},"description":rules.MARKER+"17/7777"}
        desired = rules.camel(current)
        desired.pop("id");desired.pop("direction")
        calls=[]
        def fake(*args):
            calls.append(args)
            return {"data":[current]}
        rules.run=fake
        rules.sync_target({"kind":"nsg","id":"ocid1.networksecuritygroup.test"},[desired])
        self.assertEqual(len(calls),1)

unittest.main()
