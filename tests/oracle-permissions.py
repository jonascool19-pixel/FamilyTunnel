import hashlib
import importlib.util
import json
import pathlib
import unittest

path=pathlib.Path(__file__).parents[1]/'scripts'/'oracle-permissions.py'
spec=importlib.util.spec_from_file_location('permissions',path)
permissions=importlib.util.module_from_spec(spec)
spec.loader.exec_module(permissions)

class Permissions(unittest.TestCase):
    def test_scoped_bootstrap_and_repeat(self):
        instance='ocid1.instance.oc1.example'
        root='ocid1.tenancy.oc1.example'
        groups=[];policies=[];writes=[]
        def call(region,*args):
            command=args[:3]
            if command==('iam','tenancy','get'):return {'data':{'home-region-key':'HOME'}}
            if command==('iam','region','list'):return {'data':[{'key':'HOME','name':'eu-example-1'}]}
            self.assertEqual(region,'eu-example-1','IAM writes must use the home region')
            if command==('iam','dynamic-group','list'):return {'data':groups}
            if command==('iam','policy','list'):return {'data':policies}
            self.assertEqual(args[2],'create')
            record={'name':args[args.index('--name')+1],'description':args[args.index('--description')+1]}
            if args[1]=='dynamic-group':
                record['matching-rule']=args[args.index('--matching-rule')+1];groups.append(record)
                self.assertEqual(record['matching-rule'],"ALL {instance.id = '"+instance+"'}")
            else:
                record['statements']=json.loads(args[args.index('--statements')+1]);policies.append(record)
            writes.append(command)
            return {'data':{}}
        name=permissions.bootstrap(instance,root,'eu-test-1',call)
        self.assertEqual(permissions.bootstrap(instance,root,'eu-test-1',call),name)
        self.assertEqual(len(writes),2,'Repeat must not grant duplicate access')
        text=' '.join(policies[0]['statements']).lower()
        self.assertNotIn('all-resources',text)
        self.assertNotIn('manage instances',text)
        self.assertNotIn('vnic_update',text)
        self.assertNotIn('delete',text)
        self.assertIn("request.permission = 'security_list_update'",text)
        self.assertIn("request.permission = 'network_security_group_update_security_rules'",text)

    def test_invalid_input_never_calls_oracle(self):
        def forbidden(*args):self.fail('Oracle invoked for invalid input')
        with self.assertRaises(ValueError):
            permissions.bootstrap("ocid1.instance.bad'; command",'ocid1.tenancy.example','eu-test-1',forbidden)

    def test_foreign_matching_name_is_not_overwritten(self):
        instance='ocid1.instance.oc1.example';name='FamilyTunnel-'+hashlib.sha256(instance.encode()).hexdigest()[:12]
        def call(region,*args):
            if args[:3]==('iam','tenancy','get'):return {'data':{'home-region-key':'HOME'}}
            if args[:3]==('iam','region','list'):return {'data':[{'key':'HOME','name':'eu-example-1'}]}
            self.assertEqual(args[:3],('iam','dynamic-group','list'))
            return {'data':[{'name':name,'description':'Someone else owns this','matching-rule':'ALL {}'}]}
        with self.assertRaises(ValueError):permissions.bootstrap(instance,'ocid1.tenancy.example','eu-test-1',call)

unittest.main()
