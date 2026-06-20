const fs = require('fs');

function mergeLocale(file, newKeys) {
  const locale = JSON.parse(fs.readFileSync(file, 'utf8'));
  if (!locale.setting) locale.setting = {};
  function deepMerge(target, source) {
    for (const [k, v] of Object.entries(source)) {
      if (typeof v === 'object' && v !== null && !Array.isArray(v)) {
        if (!target[k]) target[k] = {};
        deepMerge(target[k], v);
      } else if (!target[k]) {
        target[k] = v;
      }
    }
  }
  deepMerge(locale.setting, newKeys);
  fs.writeFileSync(file, JSON.stringify(locale, null, 2) + '\n');
  console.log(file + ': setting keys now ' + Object.keys(locale.setting).length);
}

const zh_twofa = {
  title: '两步验证 (2FA)', description: '使用身份验证器应用增强账户安全',
  status: '状态', statusEnabled: '已启用', statusDisabled: '未启用',
  enable: '启用 2FA', disable: '禁用 2FA', settingUp: '正在设置...',
  setupInstructions: '使用身份验证器应用扫描二维码，或手动输入密钥',
  qrCode: '二维码', secretKey: '密钥', verificationCode: '验证码',
  codePlaceholder: '输入 6 位验证码', verify: '验证', verifying: '验证中...',
  enableSuccess: '2FA 已启用', enableFailed: '启用失败',
  confirmDisable: '确认禁用两步验证？', disableInstructions: '输入验证码或备份码以禁用',
  disableSuccess: '2FA 已禁用', disableFailed: '禁用失败',
  backupCodes: '备份码', backupCodesHint: '请妥善保管备份码，每个只能使用一次',
  backupCodesRemaining: '剩余 {count} 个备份码', copyCodes: '复制备份码',
  copied: '已复制', copyFailed: '复制失败', newBackupCodes: '新的备份码',
  regenCodes: '重新生成备份码', regenInstructions: '输入验证码以重新生成备份码',
  regenSuccess: '备份码已重新生成', regenFailed: '重新生成失败',
  confirm: '确认', confirmRegen: '确认重新生成', dismiss: '关闭'
};

const en_twofa = {
  title: 'Two-Factor Authentication (2FA)', description: 'Enhance account security with an authenticator app',
  status: 'Status', statusEnabled: 'Enabled', statusDisabled: 'Not enabled',
  enable: 'Enable 2FA', disable: 'Disable 2FA', settingUp: 'Setting up...',
  setupInstructions: 'Scan the QR code with your authenticator app, or enter the secret key manually',
  qrCode: 'QR Code', secretKey: 'Secret Key', verificationCode: 'Verification Code',
  codePlaceholder: 'Enter 6-digit code', verify: 'Verify', verifying: 'Verifying...',
  enableSuccess: '2FA enabled', enableFailed: 'Enable failed',
  confirmDisable: 'Confirm disable two-factor authentication?',
  disableInstructions: 'Enter verification code or backup code to disable',
  disableSuccess: '2FA disabled', disableFailed: 'Disable failed',
  backupCodes: 'Backup Codes', backupCodesHint: 'Keep your backup codes safe, each can only be used once',
  backupCodesRemaining: '{count} backup codes remaining', copyCodes: 'Copy backup codes',
  copied: 'Copied', copyFailed: 'Copy failed', newBackupCodes: 'New backup codes',
  regenCodes: 'Regenerate backup codes', regenInstructions: 'Enter verification code to regenerate backup codes',
  regenSuccess: 'Backup codes regenerated', regenFailed: 'Regeneration failed',
  confirm: 'Confirm', confirmRegen: 'Confirm regenerate', dismiss: 'Dismiss'
};

const zht_twofa = {
  title: '兩步驗證 (2FA)', description: '使用身份驗證器應用增強帳戶安全',
  status: '狀態', statusEnabled: '已啟用', statusDisabled: '未啟用',
  enable: '啟用 2FA', disable: '停用 2FA', settingUp: '正在設定...',
  setupInstructions: '使用身份驗證器應用掃描二維碼，或手動輸入金鑰',
  qrCode: '二維碼', secretKey: '金鑰', verificationCode: '驗證碼',
  codePlaceholder: '輸入 6 位驗證碼', verify: '驗證', verifying: '驗證中...',
  enableSuccess: '2FA 已啟用', enableFailed: '啟用失敗',
  confirmDisable: '確認停用兩步驗證？', disableInstructions: '輸入驗證碼或備份碼以停用',
  disableSuccess: '2FA 已停用', disableFailed: '停用失敗',
  backupCodes: '備份碼', backupCodesHint: '請妥善保管備份碼，每個只能使用一次',
  backupCodesRemaining: '剩餘 {count} 個備份碼', copyCodes: '複製備份碼',
  copied: '已複製', copyFailed: '複製失敗', newBackupCodes: '新的備份碼',
  regenCodes: '重新生成備份碼', regenInstructions: '輸入驗證碼以重新生成備份碼',
  regenSuccess: '備份碼已重新生成', regenFailed: '重新生成失敗',
  confirm: '確認', confirmRegen: '確認重新生成', dismiss: '關閉'
};

mergeLocale('web/public/locale/zh_hans.json', {
  stripe: { title: '在线支付 · Stripe', description: '填入 Stripe 凭据并启用后，用户可通过 Stripe Checkout 在线充值。', enable: '启用 Stripe', apiKey: 'Stripe API Key', webhookSecret: 'Webhook Secret', currency: '货币（三字母代码）', minTopup: '最低充值金额', save: '保存', saved: '已保存', saveFailed: '保存失败' },
  twofa: zh_twofa,
  subscription: { title: '订阅管理', plans: '订阅方案', mySubscription: '我的订阅', purchase: '购买', price: '价格', duration: '时长', quota: '配额', noPlan: '暂无订阅方案', expiresAt: '到期时间', status: '状态', active: '生效中', expired: '已过期', cancelled: '已取消', purchaseSuccess: '购买成功', purchaseFailed: '购买失败', admin: { title: '订阅管理', createPlan: '创建方案', editPlan: '编辑方案', deletePlan: '删除方案', bindSubscription: '绑定订阅', confirmDelete: '确认删除此方案？' } }
});

mergeLocale('web/public/locale/en.json', {
  stripe: { title: 'Online Payment · Stripe', description: 'Configure Stripe payment gateway', enable: 'Enable Stripe', apiKey: 'Stripe API Key', webhookSecret: 'Webhook Secret', currency: 'Currency', minTopup: 'Minimum top-up amount', save: 'Save', saved: 'Saved', saveFailed: 'Save failed' },
  twofa: en_twofa,
  subscription: { title: 'Subscription', plans: 'Plans', mySubscription: 'My Subscription', purchase: 'Purchase', price: 'Price', duration: 'Duration', quota: 'Quota', noPlan: 'No subscription plans available', expiresAt: 'Expires at', status: 'Status', active: 'Active', expired: 'Expired', cancelled: 'Cancelled', purchaseSuccess: 'Purchase successful', purchaseFailed: 'Purchase failed', admin: { title: 'Subscription Management', createPlan: 'Create Plan', editPlan: 'Edit Plan', deletePlan: 'Delete Plan', bindSubscription: 'Bind Subscription', confirmDelete: 'Delete this plan?' } }
});

mergeLocale('web/public/locale/zh_hant.json', {
  stripe: { title: '線上支付 · Stripe', description: '設定 Stripe 支付閘道', enable: '啟用 Stripe', apiKey: 'Stripe API Key', webhookSecret: 'Webhook Secret', currency: '貨幣', minTopup: '最低儲值金額', save: '儲存', saved: '已儲存', saveFailed: '儲存失敗' },
  twofa: zht_twofa,
  subscription: { title: '訂閱管理', plans: '訂閱方案', mySubscription: '我的訂閱', purchase: '購買', price: '價格', duration: '時長', quota: '配額', noPlan: '暫無訂閱方案', expiresAt: '到期時間', status: '狀態', active: '生效中', expired: '已過期', cancelled: '已取消', purchaseSuccess: '購買成功', purchaseFailed: '購買失敗', admin: { title: '訂閱管理', createPlan: '建立方案', editPlan: '編輯方案', deletePlan: '刪除方案', bindSubscription: '綁定訂閱', confirmDelete: '確認刪除此方案？' } }
});

console.log('Done - all locale files updated with safe merge');
