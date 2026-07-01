package repository

import "github.com/redis/go-redis/v9"

var (
	weeklyQuotaReserveScript = redis.NewScript(`
		local role = ARGV[1]
		local amount = tonumber(ARGV[2])
		local owner_cap = tonumber(ARGV[3])
		local guest_cap = tonumber(ARGV[4])
		local guest_stop_hit = tonumber(ARGV[5])
		local owner_stop_hit = tonumber(ARGV[6])
		local req_ttl = tonumber(ARGV[7])
		local ledger_ttl = tonumber(ARGV[8])
		local account_id = ARGV[9]
		local reset_epoch = ARGV[10]

		local owner_settled = tonumber(redis.call('HGET', KEYS[1], 'owner_settled') or 0)
		local guest_settled = tonumber(redis.call('HGET', KEYS[1], 'guest_settled') or 0)
		local owner_reserved = tonumber(redis.call('HGET', KEYS[1], 'owner_reserved') or 0)
		local guest_reserved = tonumber(redis.call('HGET', KEYS[1], 'guest_reserved') or 0)
		local owner_effective = owner_settled + owner_reserved
		local guest_effective = guest_settled + guest_reserved
		local owner_overflow = math.max(0, owner_effective - owner_cap)
		local guest_depletion = guest_effective + owner_overflow
		local req_state = redis.call('HGET', KEYS[2], 'state')
		if req_state == 'reserved' or req_state == 'settled' then
			return {1, 'already_reserved', guest_depletion, guest_cap}
		end

		if role == 'guest' then
			if guest_stop_hit == 1 then
				return {0, 'guest_safety_line', guest_depletion, guest_cap}
			end
			if guest_depletion + amount > guest_cap then
				return {0, 'guest_pool_full', guest_depletion, guest_cap}
			end
			redis.call('HINCRBYFLOAT', KEYS[1], 'guest_reserved', amount)
		elseif role == 'owner' then
			if owner_stop_hit == 1 then
				return {0, 'owner_safety_line', guest_depletion, guest_cap}
			end
			redis.call('HINCRBYFLOAT', KEYS[1], 'owner_reserved', amount)
		else
			return redis.error_reply('invalid weekly quota role: ' .. role)
		end

		redis.call('HMSET', KEYS[2], 'account_id', account_id, 'reset_epoch', reset_epoch, 'role', role, 'amount', amount, 'state', 'reserved')
		redis.call('EXPIRE', KEYS[2], req_ttl)
		redis.call('EXPIRE', KEYS[1], ledger_ttl)
		return {1, 'ok', guest_depletion, guest_cap}
	`)

	weeklyQuotaSettleScript = redis.NewScript(`
		local state = redis.call('HGET', KEYS[2], 'state')
		if state ~= 'reserved' then
			return 0
		end
		local role = redis.call('HGET', KEYS[2], 'role')
		local amount = tonumber(redis.call('HGET', KEYS[2], 'amount') or 0)
		local actual = tonumber(ARGV[1])

		if role == 'guest' then
			local reserved = tonumber(redis.call('HGET', KEYS[1], 'guest_reserved') or 0)
			redis.call('HSET', KEYS[1], 'guest_reserved', math.max(0, reserved - amount))
			redis.call('HINCRBYFLOAT', KEYS[1], 'guest_settled', actual)
		elseif role == 'owner' then
			local reserved = tonumber(redis.call('HGET', KEYS[1], 'owner_reserved') or 0)
			redis.call('HSET', KEYS[1], 'owner_reserved', math.max(0, reserved - amount))
			redis.call('HINCRBYFLOAT', KEYS[1], 'owner_settled', actual)
		else
			return redis.error_reply('invalid weekly quota role: ' .. tostring(role))
		end

		redis.call('HSET', KEYS[2], 'state', 'settled')
		return 1
	`)

	weeklyQuotaReleaseScript = redis.NewScript(`
		local state = redis.call('HGET', KEYS[2], 'state')
		if state ~= 'reserved' then
			return 0
		end
		local role = redis.call('HGET', KEYS[2], 'role')
		local amount = tonumber(redis.call('HGET', KEYS[2], 'amount') or 0)

		if role == 'guest' then
			local reserved = tonumber(redis.call('HGET', KEYS[1], 'guest_reserved') or 0)
			redis.call('HSET', KEYS[1], 'guest_reserved', math.max(0, reserved - amount))
		elseif role == 'owner' then
			local reserved = tonumber(redis.call('HGET', KEYS[1], 'owner_reserved') or 0)
			redis.call('HSET', KEYS[1], 'owner_reserved', math.max(0, reserved - amount))
		else
			return redis.error_reply('invalid weekly quota role: ' .. tostring(role))
		end

		redis.call('HSET', KEYS[2], 'state', 'released')
		return 1
	`)

	weeklyQuotaRebuildScript = redis.NewScript(`
		redis.call('HSET', KEYS[1], 'owner_settled', ARGV[1], 'guest_settled', ARGV[2])
		redis.call('EXPIRE', KEYS[1], ARGV[3])
		return 1
	`)

	weeklyQuotaCalibrationScript = redis.NewScript(`
		redis.call('HSET', KEYS[1], 'effective_budget_usd', ARGV[1], 'last_observed_budget_usd', ARGV[2], 'last_calibrated_unix', ARGV[3])
		redis.call('EXPIRE', KEYS[1], ARGV[4])
		return 1
	`)
)
