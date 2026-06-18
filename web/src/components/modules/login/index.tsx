'use client';

import { useState } from "react"
import { motion } from "motion/react"
import { useTranslations } from 'next-intl'
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLogin, useRegister, useSendEmailCode } from "@/api/endpoints/user"
import { useAPIKeyLogin } from "@/api/endpoints/apikey"
import { isWebAuthnSupported, usePasskeyLogin, useWebAuthnStatus } from "@/api/endpoints/webauthn"
import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/api/client"
import type { BootstrapStatusResponse } from "@/api/endpoints/bootstrap"
import Logo from "@/components/modules/logo"
import { Fingerprint, KeyRound, User } from "lucide-react"
import { ParticleBackground } from "@/components/nature"
import { useIsMobile } from "@/hooks/use-mobile"
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContents,
  TabsContent,
} from "@/components/animate-ui/components/animate/tabs"

type LoginMode = 'user' | 'apikey';

export function LoginForm({ onLoginSuccess }: { onLoginSuccess?: () => void }) {
  const t = useTranslations('login')
  const [mode, setMode] = useState<LoginMode>('user')
  const [isRegister, setIsRegister] = useState(false)
  const [inviteCode, setInviteCode] = useState("")
  const [email, setEmail] = useState("")
  const [emailCode, setEmailCode] = useState("")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [apiKey, setApiKey] = useState("")
  const [error, setError] = useState<string | null>(null)
  const isMobile = useIsMobile()

  const loginMutation = useLogin()
  const registerMutation = useRegister()
  const apiKeyLoginMutation = useAPIKeyLogin()
  const passkeyLoginMutation = usePasskeyLogin()
  const webauthnStatus = useWebAuthnStatus()

  // 商业模式时开放公开注册（来自公开 bootstrap 状态）
  const { data: bootstrapStatus } = useQuery({
    queryKey: ['bootstrap', 'status'],
    queryFn: async () => apiClient.get<BootstrapStatusResponse>('/api/v1/bootstrap/status', undefined, false),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const commercialMode = bootstrapStatus?.commercial_mode === true
  const registerInviteRequired = bootstrapStatus?.register_invite_required === true
  const registerEmailRequired = bootstrapStatus?.register_email_required === true
  const sendCodeMutation = useSendEmailCode()
  const onSendCode = () => {
    if (!email.trim()) { setError('请先填写邮箱'); return }
    sendCodeMutation.mutate(email.trim(), {
      onError: (e) => setError(e instanceof Error ? e.message : '发送失败'),
    })
  }

  const passkeyAvailable =
    isWebAuthnSupported() &&
    webauthnStatus.data?.enabled &&
    webauthnStatus.data?.has_credentials

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    try {
      if (mode === 'user') {
        if (isRegister) {
          await registerMutation.mutateAsync({
            username: username.trim(),
            password,
            expire: 1440,
            invite_code: inviteCode.trim(),
            email: email.trim(),
            email_code: emailCode.trim(),
          })
        } else {
          await loginMutation.mutateAsync({
            username: username.trim(),
            password,
            expire: 1440,
          })
        }
      } else {
        await apiKeyLoginMutation.mutateAsync(apiKey)
      }

      onLoginSuccess?.()
    } catch (err: unknown) {
      const errorMessage = err instanceof Error ? err.message : t('error.generic')
      setError(errorMessage)
    }
  }

  const handlePasskeyLogin = async () => {
    setError(null)
    try {
      await passkeyLoginMutation.mutateAsync()
      onLoginSuccess?.()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('error.generic'))
    }
  }

  const isPending = loginMutation.isPending || registerMutation.isPending || apiKeyLoginMutation.isPending || passkeyLoginMutation.isPending

  const handleModeChange = (value: string) => {
    setMode(value as LoginMode)
    setError(null)
  }

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1] }}
      className="relative min-h-screen min-h-dvh flex items-center justify-center px-4 sm:px-6 py-8 text-foreground overflow-hidden"
    >
      {/* Nature: 粒子背景 */}
      {!isMobile && <ParticleBackground count={40} minOpacity={0.08} maxOpacity={0.25} />}
      
      <div className="relative z-10 w-full max-w-sm">
        <div className="flex flex-col gap-6 sm:gap-8 p-5 sm:p-8 md:p-10 border-border/35 bg-card rounded-xl">
          <header className="flex flex-col items-center gap-3 sm:gap-4">
            <div className="grid size-14 sm:size-16 shrink-0 place-items-center overflow-hidden rounded-lg border-border/35 bg-card">
              <Logo size={40} />
            </div>
            <div className="flex flex-col items-center gap-1">
              <h1 className="text-2xl sm:text-3xl font-bold tracking-tight">GGZERO</h1>
              <p className="text-sm text-muted-foreground/80 font-medium">{t('welcome') || 'Welcome back'}</p>
            </div>
          </header>

            <Tabs value={mode} onValueChange={handleModeChange}>
            <TabsList className="flex w-full h-auto p-1 bg-muted/50 rounded-2xl border border-border/20">
              <TabsTrigger
                value="user"
                className="rounded-xl text-xs sm:text-sm"
              >
                <User className="w-3.5 h-3.5 sm:w-4 sm:h-4" />
                <span className="truncate">{t('mode.user')}</span>
              </TabsTrigger>
              <TabsTrigger
                value="apikey"
                className="rounded-xl text-xs sm:text-sm"
              >
                <KeyRound className="w-3.5 h-3.5 sm:w-4 sm:h-4" />
                <span className="truncate">{t('mode.apikey')}</span>
              </TabsTrigger>
            </TabsList>

            <form onSubmit={handleSubmit} className="space-y-6 pt-4">
              <TabsContents>
                <TabsContent value="user" className="space-y-5">
                  <Field>
                    <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="username">{t('username')}</FieldLabel>
                    <Input
                      id="username"
                      type="text"
                      placeholder={t('usernamePlaceholder')}
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      className="h-12 rounded-xl bg-card border-border/30"
                      autoComplete="username"
                      autoCapitalize="none"
                      autoCorrect="off"
                      spellCheck={false}
                      required={mode === 'user'}
                      disabled={isPending}
                    />
                  </Field>
                  <Field>
                    <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="password">{t('password')}</FieldLabel>
                    <Input
                      id="password"
                      type="password"
                      placeholder={t('passwordPlaceholder')}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      className="h-12 rounded-xl bg-card border-border/30"
                      autoComplete="current-password"
                      required={mode === 'user'}
                      disabled={isPending}
                    />
                  </Field>
                  {isRegister && registerEmailRequired && (
                    <>
                    <Field>
                      <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="email">邮箱</FieldLabel>
                      <div className="flex gap-2">
                        <Input
                          id="email"
                          type="email"
                          placeholder="your@email.com"
                          value={email}
                          onChange={(e) => setEmail(e.target.value)}
                          className="h-12 rounded-xl bg-card border-border/30"
                          autoCapitalize="none"
                          autoCorrect="off"
                          spellCheck={false}
                          disabled={isPending}
                        />
                        <Button
                          type="button"
                          variant="outline"
                          onClick={onSendCode}
                          disabled={sendCodeMutation.isPending || !email.trim()}
                          className="h-12 shrink-0 rounded-xl"
                        >
                          {sendCodeMutation.isPending ? '发送中' : '发送验证码'}
                        </Button>
                      </div>
                    </Field>
                    <Field>
                      <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="emailcode">验证码</FieldLabel>
                      <Input
                        id="emailcode"
                        type="text"
                        placeholder="邮箱收到的 6 位验证码"
                        value={emailCode}
                        onChange={(e) => setEmailCode(e.target.value)}
                        className="h-12 rounded-xl bg-card border-border/30"
                        autoCapitalize="none"
                        autoCorrect="off"
                        spellCheck={false}
                        disabled={isPending}
                      />
                    </Field>
                    </>
                  )}
                  {isRegister && registerInviteRequired && (
                    <Field>
                      <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="invite">邀请码</FieldLabel>
                      <Input
                        id="invite"
                        type="text"
                        placeholder="请输入邀请码"
                        value={inviteCode}
                        onChange={(e) => setInviteCode(e.target.value)}
                        className="h-12 rounded-xl bg-card border-border/30"
                        autoCapitalize="none"
                        autoCorrect="off"
                        spellCheck={false}
                        disabled={isPending}
                      />
                    </Field>
                  )}
                  {commercialMode && (
                    <button
                      type="button"
                      onClick={() => { setIsRegister((v) => !v); setError(null) }}
                      className="ml-1 self-start text-xs text-muted-foreground transition-colors hover:text-foreground"
                    >
                      {isRegister ? '已有账号？去登录' : '没有账号？注册新账号 →'}
                    </button>
                  )}
                </TabsContent>
                <TabsContent value="apikey">
                  <Field>
                    <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="apikey">{t('apikey')}</FieldLabel>
                    <Input
                      id="apikey"
                      type="password"
                      placeholder={t('apikeyPlaceholder')}
                      value={apiKey}
                      onChange={(e) => setApiKey(e.target.value)}
                      className="h-12 rounded-xl bg-card border-border/30"
                      autoComplete="off"
                      autoCapitalize="none"
                      autoCorrect="off"
                      spellCheck={false}
                      required={mode === 'apikey'}
                      disabled={isPending}
                    />
                  </Field>
                </TabsContent>
              </TabsContents>

              {error && (
                <motion.div
                  initial={{ opacity: 0, y: -5 }}
                  animate={{ opacity: 1, y: 0 }}
                  className="px-1"
                >
                  <FieldDescription className="text-destructive font-medium text-xs bg-destructive/5 p-2 rounded-lg border border-destructive/10">
                    {error}
                  </FieldDescription>
                </motion.div>
              )}

              <Button
                type="submit"
                disabled={isPending}
                className="w-full h-12 rounded-xl bg-primary text-primary-foreground hover:bg-primary/90 transition-all active:scale-[0.98]"
              >
                {isPending ? t('button.loading') : (mode === 'user' && isRegister ? '注册并进入' : t('button.submit'))}
              </Button>

              {passkeyAvailable && (
                <>
                  <div className="relative flex items-center gap-3 py-1">
                    <div className="h-px flex-1 bg-border/30" />
                    <span className="text-[11px] text-muted-foreground/60">or</span>
                    <div className="h-px flex-1 bg-border/30" />
                  </div>
                  <Button
                    type="button"
                    onClick={handlePasskeyLogin}
                    disabled={isPending}
                    variant="outline"
                    className="w-full h-12 rounded-xl border-border/40 bg-card hover:bg-muted/50 transition-all active:scale-[0.98]"
                  >
                    <Fingerprint className="w-4 h-4" />
                    {t('button.passkey')}
                  </Button>
                </>
              )}
            </form>
          </Tabs>
        </div>
      </div>
    </motion.div>
  )
}
