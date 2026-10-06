package auth

import (
	"context"
	"errors"
	"github.com/golang-jwt/jwt"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"time"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

type JwtResponse struct {
	Jwt string `json:"jwt"`
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogic) Login(req *types.LoginReq) (resp *types.Resp, err error) {
	resp = &types.Resp{}
	// 【本地修改 2026-10-07】按用户要求去掉密码校验：不再比对 secretKey，直接签发 JWT。
	// 原逻辑：if l.svcCtx.Config.Auth.AccessSecret != req.SecretKey { 401 }
	// JWT 签发与路由上的 jwt: Auth 守卫保持不变 —— 前端仍拿 token，只是不再需要输入密码。
	// ⚠️ 安全影响：任何能访问该端口的人都能控制 Docker。仅用于内网实例。
	_ = req // 请求里的 secretKey 已不再使用
	jwtToken, err := l.getJwtToken(l.svcCtx.Config.Auth.AccessSecret,
		time.Now().Unix(),
		l.svcCtx.Config.Auth.AccessExpire,
	)
	if err != nil {
		resp.Code = 500
		resp.Msg = "无法生成 token，请重试"
		resp.Data = JwtResponse{Jwt: ""}
		return resp, errors.New("生成 token出现错误，请重试")
	}
	resp.Code = 200
	resp.Msg = "success"
	resp.Data = JwtResponse{Jwt: jwtToken}
	return resp, nil
}

func (l *LoginLogic) getJwtToken(secretKey string, iat, seconds int64) (string, error) {
	claims := make(jwt.MapClaims)
	claims["iat"] = iat
	claims["exp"] = iat + seconds
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secretKey))
}
