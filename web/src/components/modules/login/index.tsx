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
import { getGitHubAuthURL } from "@/api/endpoints/oauth"
import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/api/client"
import type { BootstrapStatusResponse } from "@/api/endpoints/bootstrap"
import Logo from "@/components/modules/logo"
import { Fingerprint, Github, KeyRound, User } from "lucide-react"
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
  const tf = useTranslations('form')
  const [mode, setMode] = useState<LoginMode>('user')
  const [isRegister, setIsRegister] = useState(false)
  const [inviteCode, setInviteCode] = useState("")
  const [email, setEmail] = useState("")
  const [emailCode, setEmailCode] = useState("")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [totpCode, setTotpCode] = useState("")
  const [needsTwoFactor, setNeedsTwoFactor] = useState(false)
  const [apiKey, setApiKey] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
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
    if (!email.trim()) { setError(tf('sendCodeFirst')); return }
    sendCodeMutation.mutate(email.trim(), {
      onError: (e) => setError(e instanceof Error ? e.message : tf('sendFailed')),
    })
  }

  const passkeyAvailable =
    isWebAuthnSupported() &&
    webauthnStatus.data?.enabled &&
    webauthnStatus.data?.has_credentials

  const githubOAuthEnabled = bootstrapStatus?.github_oauth_enabled === true

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
          const data = await loginMutation.mutateAsync({
            username: username.trim(),
            password,
            totp_code: needsTwoFactor ? totpCode.trim() : undefined,
            expire: 1440,
          })
          if (data.requires_two_factor) {
            setNeedsTwoFactor(true)
            return // 等用户填 TOTP 后再次提交；不调 onLoginSuccess
          }
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

  // N-37: Real-time field validation
  const validateField = (field: string, value: string) => {
    setFieldErrors((prev) => {
      const next = { ...prev }
      switch (field) {
        case 'email':
          if (!value.trim()) next.email = tf('validation.emailRequired')
          else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) next.email = tf('validation.emailInvalid')
          else delete next.email
          break
        case 'username':
          if (!value.trim()) next.username = tf('validation.usernameRequired')
          else if (!/^[a-zA-Z0-9_]{3,20}$/.test(value)) next.username = tf('validation.usernameFormat')
          else delete next.username
          break
        case 'password':
          if (!value) next.password = tf('validation.passwordRequired')
          else if (value.length < 6) next.password = tf('validation.passwordMinLength')
          else delete next.password
          break
      }
      return next
    })
  }

  const isPending = loginMutation.isPending || registerMutation.isPending || apiKeyLoginMutation.isPending || passkeyLoginMutation.isPending
  const [githubLoading, setGithubLoading] = useState(false)

  const handleGitHubLogin = async () => {
    setError(null)
    setGithubLoading(true)
    try {
      const data = await getGitHubAuthURL()
      window.location.href = data.authorize_url
    } catch (err: unknown) {
      setGithubLoading(false)
      setError(err instanceof Error ? err.message : t('githubOAuthError'))
    }
  }

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
              <h1 className="text-2xl sm:text-3xl font-bold tracking-tight">Lodestar</h1>
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
                      onBlur={() => isRegister && validateField('username', username)}
                      className="h-12 rounded-xl bg-card border-border/30"
                      autoComplete="username"
                      autoCapitalize="none"
                      autoCorrect="off"
                      spellCheck={false}
                      required={mode === 'user'}
                      disabled={isPending}
                    />
                    {fieldErrors.username && <span className="text-[11px] text-destructive ml-1">{fieldErrors.username}</span>}
                  </Field>
                  <Field>
                    <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="password">{t('password')}</FieldLabel>
                    <Input
                      id="password"
                      type="password"
                      placeholder={t('passwordPlaceholder')}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      onBlur={() => isRegister && validateField('password', password)}
                      className="h-12 rounded-xl bg-card border-border/30"
                      autoComplete="current-password"
                      required={mode === 'user'}
                      disabled={isPending}
                    />
                    {fieldErrors.password && <span className="text-[11px] text-destructive ml-1">{fieldErrors.password}</span>}
                  </Field>
                  {needsTwoFactor && !isRegister && (
                    <Field>
                      <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="totp">{tf('totpLabel')}</FieldLabel>
                      <Input
                        id="totp"
                        type="text"
                        placeholder={tf('totpPlaceholder')}
                        value={totpCode}
                        onChange={(e) => setTotpCode(e.target.value)}
                        className="h-12 rounded-xl bg-card border-border/30"
                        autoCapitalize="none"
                        autoCorrect="off"
                        spellCheck={false}
                        autoComplete="one-time-code"
                        required
                        disabled={isPending}
                      />
                    </Field>
                  )}
                  {isRegister && registerEmailRequired && (
                    <>
                    <Field>
                      <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="email">{tf('emailLabel')}</FieldLabel>
                      <div className="flex gap-2">
                        <Input
                          id="email"
                          type="email"
                          placeholder="your@email.com"
                          value={email}
                          onChange={(e) => setEmail(e.target.value)}
                          onBlur={() => validateField('email', email)}
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
                          {sendCodeMutation.isPending ? tf('sending') : tf('sendCode')}
                        </Button>
                      </div>
                      {fieldErrors.email && <span className="text-[11px] text-destructive ml-1">{fieldErrors.email}</span>}
                    </Field>
                    <Field>
                      <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="emailcode">{tf('emailCodeLabel')}</FieldLabel>
                      <Input
                        id="emailcode"
                        type="text"
                        placeholder={tf('emailCodePlaceholder')}
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
                      <FieldLabel className="text-xs font-semibold text-muted-foreground/70 ml-1" htmlFor="invite">{tf('inviteCodeLabel')}</FieldLabel>
                      <Input
                        id="invite"
                        type="text"
                        placeholder={tf('inviteCodePlaceholder')}
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
                      {isRegister ? tf('hasAccount') : tf('noAccount')}
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
                {isPending ? t('button.loading') : (mode === 'user' && isRegister ? tf('registerAndEnter') : t('button.submit'))}
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

              {githubOAuthEnabled && (
                <>
                  {!passkeyAvailable && (
                    <div className="relative flex items-center gap-3 py-1">
                      <div className="h-px flex-1 bg-border/30" />
                      <span className="text-[11px] text-muted-foreground/60">or</span>
                      <div className="h-px flex-1 bg-border/30" />
                    </div>
                  )}
                  <Button
                    type="button"
                    onClick={handleGitHubLogin}
                    disabled={isPending || githubLoading}
                    variant="outline"
                    className="w-full h-12 rounded-xl border-border/40 bg-card hover:bg-muted/50 transition-all active:scale-[0.98]"
                  >
                    <Github className="w-4 h-4" />
                    {githubLoading ? t('button.loading') : t('button.githubOAuth')}
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
