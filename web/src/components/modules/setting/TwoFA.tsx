'use client';

import { useState } from 'react';
import { ShieldCheck, KeyRound, Eye, EyeOff, Copy, RefreshCw, AlertTriangle, CheckCircle2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/common/Toast';
import {
    useTwoFAStatus,
    useSetup2FA,
    useEnable2FA,
    useDisable2FA,
    useRegenerateBackupCodes,
} from '@/api/endpoints/twofa';

export function SettingTwoFA() {
    const t = useTranslations('setting');
    const { data: status, isLoading: statusLoading } = useTwoFAStatus();
    const setupMutation = useSetup2FA();
    const enableMutation = useEnable2FA();
    const disableMutation = useDisable2FA();
    const regenMutation = useRegenerateBackupCodes();

    const [verifyCode, setVerifyCode] = useState('');
    const [disableCode, setDisableCode] = useState('');
    const [showDisableInput, setShowDisableInput] = useState(false);
    const [regenCode, setRegenCode] = useState('');
    const [showRegenInput, setShowRegenInput] = useState(false);
    const [newBackupCodes, setNewBackupCodes] = useState<string[] | null>(null);
    const [showSecret, setShowSecret] = useState(false);

    const setupData = setupMutation.data;
    const isEnabled = status?.enabled ?? false;

    const handleSetup = () => {
        setVerifyCode('');
        setNewBackupCodes(null);
        setupMutation.mutate();
    };

    const handleEnable = () => {
        if (!verifyCode.trim() || !setupData) return;
        enableMutation.mutate(
            { code: verifyCode.trim(), backup_codes: setupData.backup_codes },
            {
                onSuccess: () => {
                    toast.success(t('twofa.enableSuccess'));
                    setVerifyCode('');
                },
                onError: () => toast.error(t('twofa.enableFailed')),
            },
        );
    };

    const handleDisable = () => {
        if (!disableCode.trim()) return;
        disableMutation.mutate(disableCode.trim(), {
            onSuccess: () => {
                toast.success(t('twofa.disableSuccess'));
                setShowDisableInput(false);
                setDisableCode('');
            },
            onError: () => toast.error(t('twofa.disableFailed')),
        });
    };

    const handleRegen = () => {
        if (!regenCode.trim()) return;
        regenMutation.mutate(regenCode.trim(), {
            onSuccess: (codes) => {
                setNewBackupCodes(codes);
                setShowRegenInput(false);
                setRegenCode('');
                toast.success(t('twofa.regenSuccess'));
            },
            onError: () => toast.error(t('twofa.regenFailed')),
        });
    };

    const copyToClipboard = (text: string) => {
        navigator.clipboard.writeText(text).then(
            () => toast.success(t('twofa.copied')),
            () => toast.error(t('twofa.copyFailed')),
        );
    };

    return (
        <div className="relative overflow-hidden rounded-xl border-border/35 bg-card p-4 sm:p-6 text-card-foreground shadow-md">
            <div className="space-y-4 sm:space-y-5">
                <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                    <div className="space-y-1.5">
                        <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                            <ShieldCheck className="h-5 w-5" />
                            {t('twofa.title')}
                        </h2>
                        <p className="text-sm text-muted-foreground">{t('twofa.description')}</p>
                    </div>
                </div>

                <div className="space-y-4 rounded-xl border border-border/30 bg-muted/10 p-4">
                    {/* Status display */}
                    <div className="flex items-center justify-between">
                        <span className="text-sm font-medium text-card-foreground">{t('twofa.status')}</span>
                        {statusLoading ? (
                            <span className="text-xs text-muted-foreground">...</span>
                        ) : isEnabled ? (
                            <span className="flex items-center gap-1.5 text-xs font-medium text-green-600">
                                <CheckCircle2 className="size-3.5" />
                                {t('twofa.statusEnabled')}
                            </span>
                        ) : (
                            <span className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                                <AlertTriangle className="size-3.5" />
                                {t('twofa.statusDisabled')}
                            </span>
                        )}
                    </div>

                    {isEnabled && !statusLoading && (
                        <div className="flex items-center justify-between">
                            <span className="text-sm text-muted-foreground">{t('twofa.backupCodesRemaining')}</span>
                            <span className="text-sm font-medium">{status?.backup_codes_remaining ?? 0}</span>
                        </div>
                    )}

                    {/* Setup flow */}
                    {!isEnabled && !setupData && (
                        <Button
                            onClick={handleSetup}
                            disabled={setupMutation.isPending || statusLoading}
                            className="rounded-lg"
                        >
                            {setupMutation.isPending ? t('twofa.settingUp') : t('twofa.enable')}
                        </Button>
                    )}

                    {/* QR code / secret display */}
                    {!isEnabled && setupData && (
                        <div className="space-y-4">
                            <p className="text-xs text-muted-foreground">{t('twofa.setupInstructions')}</p>

                            {/* QR code image */}
                            {setupData.qr_code && (
                                <div className="flex justify-center">
                                    <img
                                        src={setupData.qr_code}
                                        alt={t('twofa.qrCode')}
                                        className="rounded-lg border border-border/30 bg-white p-2"
                                        width={180}
                                        height={180}
                                    />
                                </div>
                            )}

                            {/* Secret key */}
                            <div className="space-y-1.5">
                                <label className="text-xs font-semibold text-muted-foreground">{t('twofa.secretKey')}</label>
                                <div className="flex items-center gap-2">
                                    <code className="flex-1 rounded-lg border border-border/30 bg-muted/20 px-3 py-2 text-xs font-mono break-all">
                                        {showSecret ? setupData.secret : '●●●●●●●●●●●●●●●●'}
                                    </code>
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="size-8 shrink-0"
                                        onClick={() => setShowSecret(!showSecret)}
                                    >
                                        {showSecret ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                                    </Button>
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="size-8 shrink-0"
                                        onClick={() => copyToClipboard(setupData.secret)}
                                    >
                                        <Copy className="size-4" />
                                    </Button>
                                </div>
                            </div>

                            {/* Backup codes */}
                            {setupData.backup_codes.length > 0 && (
                                <div className="space-y-1.5">
                                    <div className="flex items-center justify-between">
                                        <label className="text-xs font-semibold text-muted-foreground">{t('twofa.backupCodes')}</label>
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            className="h-6 text-xs"
                                            onClick={() => copyToClipboard(setupData.backup_codes.join('\n'))}
                                        >
                                            <Copy className="size-3 mr-1" />
                                            {t('twofa.copyCodes')}
                                        </Button>
                                    </div>
                                    <div className="rounded-lg border border-border/30 bg-muted/20 p-3">
                                        <div className="grid grid-cols-2 gap-1.5">
                                            {setupData.backup_codes.map((code) => (
                                                <code key={code} className="text-xs font-mono">{code}</code>
                                            ))}
                                        </div>
                                    </div>
                                    <p className="text-[11px] text-muted-foreground/70">{t('twofa.backupCodesHint')}</p>
                                </div>
                            )}

                            {/* Verification code input */}
                            <div className="space-y-1.5">
                                <label className="text-xs font-semibold text-muted-foreground">{t('twofa.verificationCode')}</label>
                                <div className="flex gap-2">
                                    <Input
                                        value={verifyCode}
                                        onChange={(e) => setVerifyCode(e.target.value)}
                                        placeholder={t('twofa.codePlaceholder')}
                                        className="rounded-lg flex-1"
                                        maxLength={6}
                                        inputMode="numeric"
                                        autoComplete="one-time-code"
                                    />
                                    <Button
                                        onClick={handleEnable}
                                        disabled={enableMutation.isPending || !verifyCode.trim()}
                                        className="rounded-lg"
                                    >
                                        {enableMutation.isPending ? t('twofa.verifying') : t('twofa.confirm')}
                                    </Button>
                                </div>
                            </div>
                        </div>
                    )}

                    {/* Disable flow */}
                    {isEnabled && !showDisableInput && (
                        <div className="flex flex-wrap gap-2">
                            <Button
                                variant="destructive"
                                onClick={() => setShowDisableInput(true)}
                                className="rounded-lg"
                            >
                                {t('twofa.disable')}
                            </Button>
                            {!showRegenInput && (
                                <Button
                                    variant="outline"
                                    onClick={() => setShowRegenInput(true)}
                                    className="rounded-lg"
                                >
                                    <RefreshCw className="size-4 mr-1.5" />
                                    {t('twofa.regenCodes')}
                                </Button>
                            )}
                        </div>
                    )}

                    {isEnabled && showDisableInput && (
                        <div className="space-y-3">
                            <p className="text-xs text-muted-foreground">{t('twofa.disableInstructions')}</p>
                            <div className="flex gap-2">
                                <Input
                                    value={disableCode}
                                    onChange={(e) => setDisableCode(e.target.value)}
                                    placeholder={t('twofa.codePlaceholder')}
                                    className="rounded-lg flex-1"
                                    maxLength={8}
                                    inputMode="numeric"
                                    autoComplete="one-time-code"
                                />
                                <Button
                                    variant="destructive"
                                    onClick={handleDisable}
                                    disabled={disableMutation.isPending || !disableCode.trim()}
                                    className="rounded-lg"
                                >
                                    {disableMutation.isPending ? t('twofa.verifying') : t('twofa.confirmDisable')}
                                </Button>
                                <Button
                                    variant="ghost"
                                    onClick={() => { setShowDisableInput(false); setDisableCode(''); }}
                                    className="rounded-lg"
                                >
                                    {t('form.cancel')}
                                </Button>
                            </div>
                        </div>
                    )}

                    {/* Regenerate backup codes */}
                    {isEnabled && showRegenInput && (
                        <div className="space-y-3">
                            <p className="text-xs text-muted-foreground">{t('twofa.regenInstructions')}</p>
                            <div className="flex gap-2">
                                <Input
                                    value={regenCode}
                                    onChange={(e) => setRegenCode(e.target.value)}
                                    placeholder={t('twofa.codePlaceholder')}
                                    className="rounded-lg flex-1"
                                    maxLength={8}
                                    inputMode="numeric"
                                    autoComplete="one-time-code"
                                />
                                <Button
                                    onClick={handleRegen}
                                    disabled={regenMutation.isPending || !regenCode.trim()}
                                    className="rounded-lg"
                                >
                                    {regenMutation.isPending ? t('twofa.verifying') : t('twofa.confirmRegen')}
                                </Button>
                                <Button
                                    variant="ghost"
                                    onClick={() => { setShowRegenInput(false); setRegenCode(''); }}
                                    className="rounded-lg"
                                >
                                    {t('form.cancel')}
                                </Button>
                            </div>
                        </div>
                    )}

                    {/* New backup codes after regeneration */}
                    {newBackupCodes && (
                        <div className="space-y-3">
                            <p className="text-sm font-medium text-green-600">{t('twofa.regenSuccess')}</p>
                            <div className="space-y-1.5">
                                <div className="flex items-center justify-between">
                                    <label className="text-xs font-semibold text-muted-foreground">{t('twofa.newBackupCodes')}</label>
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        className="h-6 text-xs"
                                        onClick={() => copyToClipboard(newBackupCodes.join('\n'))}
                                    >
                                        <Copy className="size-3 mr-1" />
                                        {t('twofa.copyCodes')}
                                    </Button>
                                </div>
                                <div className="rounded-lg border border-border/30 bg-muted/20 p-3">
                                    <div className="grid grid-cols-2 gap-1.5">
                                        {newBackupCodes.map((code) => (
                                            <code key={code} className="text-xs font-mono">{code}</code>
                                        ))}
                                    </div>
                                </div>
                                <p className="text-[11px] text-muted-foreground/70">{t('twofa.backupCodesHint')}</p>
                            </div>
                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => setNewBackupCodes(null)}
                                className="rounded-lg text-xs"
                            >
                                {t('twofa.dismiss')}
                            </Button>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
