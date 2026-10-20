package redact

// FakeSecrets are invented credentials in real formats, for tests in all packages.
var FakeSecrets = map[string]string{
	"aws":       "AKIAZ7Q3XW4YRT2LKP6M",
	"github":    "ghp_9fK2mQx7Lp4Rt8Vz1Wb6Nc3Hd5Js0Ya2EuTi",
	"anthropic": "sk-ant-api03-Xq7vL2pR9tK4mW8zN1bC6dF3hJ5gS0aE7uY2iO4lP9kQ3rT6wV1xZ8nM5bH2cG4fD7sA0eJ3kL6mN9pQ2rS5tU8vW1xY4zAA",
	"openai":    "sk-proj-Rt7Yq2Lm9Xp4Kv8Wz3Nb6Hc1Jd5Fs0Ga7Ue2Ti4Oy9Pl3Mk6Nj8Bh1Vg5Cf2Dx7Sw0Ea3Qr6Zt9Lu4Iy7Om2Pn5Kb8Jv1Hc3",
	"pem":       "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA3Tz2mr7SZiAMfQyuvBjM9Oi8TjVxQsq3Lr2Kp9Wx4Yz7Nb6Hc1Jd5\nFs0Ga7Ue2Ti4Oy9Pl3Mk6Nj8Bh1Vg5Cf2Dx7Sw0Ea3Qr6Zt9Lu4Iy7Om2Pn5Kb8Jv1Hc3Rt7Y\nq2Lm9Xp4Kv8Wz3Nb6Hc1Jd5Fs0Ga7Ue2Ti4Oy9Pl3Mk6Nj8Bh1Vg5Cf2Dx7Sw0Ea3Qr6Zt9\n-----END RSA PRIVATE KEY-----",
}
