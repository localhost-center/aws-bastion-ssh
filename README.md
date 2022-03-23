## How to use
bastion host로 사용되는 인스턴스에 Tag를 지정합니다. `role:bastion` 은 디폴트 값이며, 원하시는 Tag로 지정이 가능합니다.


인스턴스 ID값으로 직접 접속을 합니다. 

`mzssh -i i-0eaa4d1c7f350216e`

Bastion host에 사용자 지정 Tag로 접근합니다. 예 - `job:bastion-special`

`mzssh --tag job:bastion-special`

기본 Bastion host을 이용해 터널링을 합니다.

`mzssh -t somedatabase.example.com:5432`

사용자 지정 인스턴스 ID를 이용해 터널링을 합니다.

`mzssh -i i-0eaa4d1c7f350216e -t somedatabase.example.com:5432`

bastion을 통해 ssh 접속을 합니다.

`mzssh -d i-0eaa4d1c7f350216e`

사용자 지정 UserName이나 port로 접근합니다.

`mzssh -d ubuntu@i-0eaa4d1c7f350216e:2222`