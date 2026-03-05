# 🚀 Blockora Blockchain (BORA)

**Live URL:** https://blockora-core--ajaychovatiya25.replit.app

## ✅ Status: PRODUCTION READY

| Feature | Status |
|---------|--------|
| Genesis Block | ✅ Active |
| PoP Mining | ✅ Working |
| 24h Sessions | ✅ Active |
| Reward System | ✅ 100 BORA/test |
| REST API | ✅ Public |

## 🎯 Quick Test

```bash
# Check status
curl https://blockora-core--ajaychovatiya25.replit.app/api/status

# Start mining
curl -X POST https://blockora-core--ajaychovatiya25.replit.app/api/pop/start \
  -H "Content-Type: application/json" \
  -d '{"address":"BxUser001"}'

# Complete mining
curl -X POST https://blockora-core--ajaychovatiya25.replit.app/api/pop/complete \
  -H "Content-Type: application/json" \
  -d '{"address":"BxUser001","base_rate":100}'

